package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"ai-engine/cmd/web"
	"ai-engine/internal/providers"
)

func (s *Server) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.DashboardHandler)
	mux.HandleFunc("GET /health", s.HealthHandler)
	mux.HandleFunc("GET /api/telemetry/stream", s.TelemetryStreamHandler)
	mux.HandleFunc("POST /api/nodes/{id}/drain", s.DrainNodeHandler)
	mux.HandleFunc("GET /api/clusters/provision", s.ProvisionModalHandler)
	mux.HandleFunc("GET /api/clusters/provision/close", s.CloseModalHandler)
	mux.HandleFunc("POST /api/clusters", s.ProvisionClusterHandler)

	fileServer := http.FileServer(http.FS(web.Files))
	mux.Handle("/assets/", fileServer)

	return s.corsMiddleware(mux)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "false")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.Dashboard(s.fleet.Nodes())).ServeHTTP(w, r)
}

func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// DrainNodeHandler marks the node Draining and returns the updated card so
// htmx can swap it in place. A real deployment routes this through the
// provider adapter, which cordons/drains via CAPI.
func (s *Server) DrainNodeHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := s.fleet.Drain(id)
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	templ.Handler(web.GPUCard(node)).ServeHTTP(w, r)
}

func (s *Server) ProvisionModalHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.ProvisionModal(providers.List())).ServeHTTP(w, r)
}

func (s *Server) CloseModalHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) ProvisionClusterHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	count, _ := strconv.Atoi(r.FormValue("count"))
	spec := providers.NodePoolSpec{
		Name:     r.FormValue("name"),
		Provider: r.FormValue("provider"),
		GPUModel: r.FormValue("gpu_model"),
		Count:    max(1, min(count, 8)),
	}

	p, err := providers.Get(spec.Provider)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := p.Provision(r.Context(), spec); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var buf bytes.Buffer
	for _, n := range s.fleet.Nodes() {
		if strings.HasPrefix(n.ID, spec.Name+"-") {
			web.GPUCard(n).Render(r.Context(), &buf)
		}
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(buf.Bytes())
}

// TelemetryStreamHandler streams per-node GPUCard fragments as SSE events.
// Each card carries hx-swap-oob so a single stream patches cards in place.
func (s *Server) TelemetryStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := s.broadcaster.Subscribe()
	defer s.broadcaster.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			// SSE data fields cannot contain raw newlines; the rendered
			// fragment is single-line safe after this strip.
			payload = strings.ReplaceAll(payload, "\n", "")
			fmt.Fprintf(w, "event: gpu-metrics\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

// runTelemetry mutates fleet metrics on a tick and broadcasts each updated
// node as an OOB card fragment. Milestone 3 replaces MutateMetrics with real
// DCGM scrapes.
func (s *Server) runTelemetry(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fleet.MutateMetrics()
			for _, node := range s.fleet.Nodes() {
				var buf bytes.Buffer
				if err := web.GPUCardOOB(node).Render(ctx, &buf); err != nil {
					log.Printf("render gpu card: %v", err)
					continue
				}
				s.broadcaster.Publish(buf.String())
			}
		}
	}
}
