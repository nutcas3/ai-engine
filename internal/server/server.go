package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"ai-engine/cmd/web"
	"ai-engine/internal/fleet"
	"ai-engine/internal/providers"
	"ai-engine/internal/telemetry"
)

type Server struct {
	port        int
	fleet       *fleet.Manager
	broadcaster *telemetry.Broadcaster
	brand       web.Brand
}

func NewServer(ctx context.Context) *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	s := &Server{
		port:        port,
		fleet:       fleet.NewManager(),
		broadcaster: telemetry.NewBroadcaster(),
		brand:       web.LoadBrand(),
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
