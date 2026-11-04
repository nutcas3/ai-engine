package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"ai-engine/cmd/web"
	"ai-engine/internal/fleet"
	"ai-engine/internal/providers"
	"ai-engine/internal/store"
	"ai-engine/internal/telemetry"
	"ai-engine/internal/worker"
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
			go worker.New(st, s.fleet).Run(ctx)
			log.Println("durable store + outbox processor enabled")
		}
	}

	// Register the fleet-bound mock provider (CAPI stubs self-register in
	// the providers package init).
	providers.Register(providers.NewMock(s.fleet))

	go s.runTelemetry(ctx)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}
