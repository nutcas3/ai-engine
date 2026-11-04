// Package store provides durable state for the control plane: resources
// (clusters, node pools) plus the workflow_outbox that decouples HTTP
// handlers from the provisioning orchestrator — NVKE's outbox pattern.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// Resource is a durable copy of a domain object (cluster, nodePool), stored
// as a JSON payload with kind+key addressing.
type Resource struct {
	bun.BaseModel `bun:"table:resources"`
	ID            int64     `bun:",pk,autoincrement"`
	Kind          string    `bun:",notnull"` // cluster | nodepool
	Key           string    `bun:",notnull"` // api ID or name
	Payload       string    `bun:"type:jsonb,notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// OutboxRow is one pending workflow unit the processor executes.
type OutboxRow struct {
	bun.BaseModel `bun:"table:workflow_outbox"`
	ID            int64      `bun:",pk,autoincrement"`
	Type          string     `bun:",notnull"` // cluster.provision | nodepool.provision
	Payload       string     `bun:"type:jsonb,notnull"`
	Status        string     `bun:",notnull,default:'pending'"` // pending|running|done|failed
	Attempts      int        `bun:",notnull,default:0"`
	NextAttemptAt time.Time  `bun:",nullzero,notnull,default:current_timestamp"`
	LastError     string     `bun:",nullzero"`
	CreatedAt     time.Time  `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt     *time.Time `bun:",nullzero"`
}

// Store wraps the Bun database handle.
type Store struct {
	db *bun.DB
}

// Open connects to Postgres via DATABASE_URL-style DSN and creates schema.
func Open(dsn string) (*Store, error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS resources (
			id BIGSERIAL PRIMARY KEY,
			kind TEXT NOT NULL,
			key TEXT NOT NULL,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (kind, key)
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_outbox (
			id BIGSERIAL PRIMARY KEY,
			type TEXT NOT NULL,
			payload JSONB NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INT NOT NULL DEFAULT 0,
			next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS workflow_outbox_pending
			ON workflow_outbox (status, next_attempt_at) WHERE status = 'pending'`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// Enqueue writes a resource record and its outbox row in ONE transaction —
// the atomicity guarantee that defines the outbox pattern.
func (s *Store) Enqueue(ctx context.Context, kind, key string, resource, job any) error {
	rjson, err := json.Marshal(resource)
	if err != nil {
		return err
	}
	jjson, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&Resource{Kind: kind, Key: key, Payload: string(rjson)}).
			On("CONFLICT (kind, key) DO UPDATE SET payload = EXCLUDED.payload, updated_at = now()").
			Exec(ctx)
		if err != nil {
			return err
		}
		jobType, _ := job.(map[string]any)["type"].(string)
		_, err = tx.NewInsert().Model(&OutboxRow{Type: jobType, Payload: string(jjson)}).Exec(ctx)
		return err
	})
}

// ClaimPending atomically claims up to n pending rows, returning them marked
// running — FOR UPDATE SKIP LOCKED keeps concurrent processors disjoint.
func (s *Store) ClaimPending(ctx context.Context, n int) ([]OutboxRow, error) {
	var rows []OutboxRow
	_, err := s.db.NewUpdate().Model((*OutboxRow)(nil)).
		Set("status = 'running', attempts = attempts + 1, updated_at = now()").
		Where("id IN (?)", s.db.NewSelect().Table("workflow_outbox").
			Column("id").Where("status = 'pending'").Where("next_attempt_at <= now()").
			Order("id").Limit(int64(n)).For("UPDATE SKIP LOCKED")).
		Returning("*").Exec(ctx, &rows)
	return rows, err
}

// Complete marks a claimed row done.
func (s *Store) Complete(ctx context.Context, id int64) error {
	_, err := s.db.NewUpdate().Model((*OutboxRow)(nil)).
		Set("status = 'done', updated_at = now()").Where("id = ?", id).Exec(ctx)
	return err
}

// Fail requeues the row with exponential backoff (attempts² minutes) or marks
// it failed after 8 attempts.
func (s *Store) Fail(ctx context.Context, id int64, attempts int, cause error) error {
	if attempts >= 8 {
		_, err := s.db.NewUpdate().Model((*OutboxRow)(nil)).
			Set("status = 'failed', last_error = ?, updated_at = now()", cause.Error()).
			Where("id = ?", id).Exec(ctx)
		return err
	}
	backoff := time.Duration(attempts*attempts) * time.Minute
	_, err := s.db.NewUpdate().Model((*OutboxRow)(nil)).
		Set("status = 'pending', last_error = ?, next_attempt_at = now() + ?::interval, updated_at = now()",
			cause.Error(), backoff.String()).Where("id = ?", id).Exec(ctx)
	return err
}

// UpdateResource rewrites a stored payload (e.g. conditions after provision).
func (s *Store) UpdateResource(ctx context.Context, kind, key string, resource any) error {
	rjson, err := json.Marshal(resource)
	if err != nil {
		return err
	}
	_, err = s.db.NewUpdate().Model((*Resource)(nil)).
		Set("payload = ?, updated_at = now()", string(rjson)).
		Where("kind = ? AND key = ?", kind, key).Exec(ctx)
	return err
}

// Close releases the database pool.
func (s *Store) Close() error { return s.db.Close() }
