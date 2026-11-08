package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/nutcas3/ai-engine/cmd/web"
	"github.com/nutcas3/ai-engine/internal/api"
	"github.com/nutcas3/ai-engine/internal/auth"
	"github.com/nutcas3/ai-engine/internal/errs"
	"github.com/nutcas3/ai-engine/internal/fleet"
	"github.com/nutcas3/ai-engine/internal/providers"
	"github.com/nutcas3/ai-engine/internal/remediation"
	"github.com/nutcas3/ai-engine/internal/store"
	"github.com/nutcas3/ai-engine/internal/telemetry"
	"github.com/nutcas3/ai-engine/internal/worker"
)

type Server struct {
	port        int
	fleet       *fleet.Manager
	broadcaster *telemetry.Broadcaster
	brand       web.Brand
	store       *store.Store
}

func NewServer(ctx context.Context) *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	s := &Server{
		port:        port,
		fleet:       fleet.NewManager(),
		broadcaster: telemetry.NewBroadcaster(),
		brand:       web.LoadBrand(),
	}
	// Durable state + outbox processor when DATABASE_URL is set; in-memory
	// only otherwise (local dev).
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		if st, err := store.Open(dsn); err != nil {
			log.Printf("DATABASE_URL set but store unavailable (%v) — continuing in-memory", err)
		} else {
			s.store = st
			restored := 0
			for _, blob := range mustList(ctx, st, "cluster") {
				var c api.Cluster
				if json.Unmarshal(blob, &c) == nil {
					s.fleet.RegisterCluster(c)
					restored++
				}
			}
			for _, blob := range mustList(ctx, st, "nodepool") {
				var p api.NodePool
				if json.Unmarshal(blob, &p) == nil {
					s.fleet.AddNodePool(p.ClusterID, p)
					restored++
				}
			}
			if restored > 0 {
				log.Printf("restored %d resources from store", restored)
			}
			go worker.New(st, s.fleet).Run(ctx)
			log.Println("durable store + outbox processor enabled")
		}
	}

	// Register the fleet-bound mock provider (CAPI stubs self-register in
	// the providers package init).
	providers.Register(providers.NewMock(s.fleet))

	go s.runTelemetry(ctx)
	go remediation.New(s.fleet).Run(ctx)

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.handler(),
		IdleTimeout:       time.Minute,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// handler wraps the routes with panic recovery at the boundary and OIDC
// auth on mutating /api/* when OIDC_ISSUER is configured.
func (s *Server) handler() http.Handler {
	h := s.RegisterRoutes()
	if mw := auth.New(os.Getenv("OIDC_ISSUER"), os.Getenv("OIDC_AUDIENCE")); mw != nil {
		h = mw.Require(h)
	}
	return recoverHandler(h)
}

// recoverHandler converts a panicking handler into a 500 instead of crashing
// the whole server — the boundary safety net, not an error mechanism.
func recoverHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				errs.Write(w, errs.E(r.URL.Path, errs.Internal, fmt.Errorf("%v", rec), "internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// mustList returns stored payloads for a kind, empty on error (restore is
// best-effort — the in-memory path still works if a row is corrupt).
func mustList(ctx context.Context, st *store.Store, kind string) [][]byte {
	rows, err := st.ListResources(ctx, kind)
	if err != nil {
		log.Printf("restore %s: %v", kind, err)
		return nil
	}
	return rows
}
