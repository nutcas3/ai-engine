// Package worker runs the outbox processor: it polls workflow_outbox, claims
// rows, and drives the actual provisioning — handlers never call the
// orchestrator directly.
package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"ai-engine/internal/api"
	"ai-engine/internal/fleet"
	"ai-engine/internal/providers"
	"ai-engine/internal/store"
)

// Processor drains workflow_outbox rows onto provider adapters.
type Processor struct {
	store *store.Store
	fleet *fleet.Manager
}

func New(st *store.Store, f *fleet.Manager) *Processor {
	return &Processor{store: st, fleet: f}
}

// Run polls until ctx is cancelled — call in a goroutine.
func (p *Processor) Run(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.drain(ctx)
		}
	}
}

func (p *Processor) drain(ctx context.Context) {
	rows, err := p.store.ClaimPending(ctx, 8)
	if err != nil {
		log.Printf("outbox: claim: %v", err)
		return
	}
	for _, row := range rows {
		if err := p.dispatch(ctx, row); err != nil {
			log.Printf("outbox: %s #%d failed: %v", row.Type, row.ID, err)
			_ = p.store.Fail(ctx, row.ID, row.Attempts, err)
			continue
		}
		_ = p.store.Complete(ctx, row.ID)
	}
}

func (p *Processor) dispatch(ctx context.Context, row store.OutboxRow) error {
	var job struct {
		Type    string              `json:"type"`
		Cluster *api.CreateClusterRequest `json:"cluster,omitempty"`
		Pool    *api.NodePoolCreateSpec   `json:"nodePool,omitempty"`
	}
	if err := json.Unmarshal([]byte(row.Payload), &job); err != nil {
		return err
	}
	switch row.Type {
	case "cluster.provision":
		prov, err := providers.Get(job.Cluster.Provider.Kind)
		if err != nil {
			return err
		}
		cp, ok := prov.(providers.ClusterProvisioner)
		if !ok {
			return providers.ErrNotConfigured
		}
		cluster, err := cp.ProvisionCluster(ctx, *job.Cluster)
		if err != nil {
			return err
		}
		p.fleet.RegisterCluster(*cluster)
		return p.store.UpdateResource(ctx, "cluster", cluster.ID, cluster)
	case "nodepool.provision":
		prov, err := providers.Get(job.Pool.Provider.Kind)
		if err != nil {
			return err
		}
		res, err := prov.ProvisionNodePool(ctx, *job.Pool)
		if err != nil {
			return err
		}
		res.Pool.ClusterID = "cluster-gpu-fleet"
		p.fleet.AddNodePool(res.Pool.ClusterID, res.Pool)
		return p.store.UpdateResource(ctx, "nodepool", res.Pool.ID, res.Pool)
	default:
		return &unknownJobType{row.Type}
	}
}

type unknownJobType struct{ t string }

func (e *unknownJobType) Error() string { return "unknown job type: " + e.t }
