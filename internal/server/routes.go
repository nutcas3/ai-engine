package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"ai-engine/cmd/web"
	"ai-engine/internal/api"
	"ai-engine/internal/providers"
)

func (s *Server) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	// UI pages
	mux.HandleFunc("GET /{$}", s.DashboardHandler)
	mux.HandleFunc("GET /clusters", s.ClustersPageHandler)
	mux.HandleFunc("GET /nodepools", s.NodePoolsPageHandler)
	mux.HandleFunc("GET /nodes", s.NodesPageHandler)
	mux.HandleFunc("GET /releases", s.ReleasesPageHandler)
	mux.HandleFunc("GET /inference", s.InferencePageHandler)
	mux.HandleFunc("GET /finops", s.FinOpsPageHandler)
	mux.HandleFunc("GET /providers", s.ProvidersPageHandler)
	mux.HandleFunc("GET /settings", s.SettingsPageHandler)
	mux.HandleFunc("GET /health", s.HealthHandler)
	mux.HandleFunc("GET /api/telemetry/stream", s.TelemetryStreamHandler)
	mux.HandleFunc("GET /api/clusters/provision", s.ProvisionModalHandler)
	mux.HandleFunc("GET /api/clusters/provision/close", s.CloseModalHandler)

	// NVKE-shaped REST surface. htmx calls carry HX-Request and get HTML
	// fragments back; API clients get JSON.
	mux.HandleFunc("GET /api/clusters", s.ListClustersHandler)
	mux.HandleFunc("POST /api/clusters", s.CreateClusterHandler)
	mux.HandleFunc("GET /api/nodePools", s.ListNodePoolsHandler)
	mux.HandleFunc("POST /api/nodePools", s.CreateNodePoolHandler)
	mux.HandleFunc("GET /api/nodes", s.ListNodesHandler)
	mux.HandleFunc("POST /api/nodes/{id}/drain", s.DrainNodeHandler)
	mux.HandleFunc("GET /api/releases", s.ListReleasesHandler)
	mux.HandleFunc("POST /api/inference", s.CreateInferenceHandler)

	// Force revalidation so regenerated CSS/JS isn't served stale from
	// browser cache during development.
	assetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.FileServer(http.FS(web.Files)).ServeHTTP(w, r)
	})
	mux.Handle("/assets/", assetHandler)

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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func (s *Server) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.Dashboard(s.fleet.GPUNodes(), s.brand)).ServeHTTP(w, r)
}

func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListClustersHandler mirrors NVKE GET /clusters.
func (s *Server) ListClustersHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"clusters": s.fleet.Clusters()})
}

// CreateClusterHandler mirrors NVKE POST /clusters. The provider payload is
// fixed at creation, as in NVKE.
func (s *Server) CreateClusterHandler(w http.ResponseWriter, r *http.Request) {
	var req api.CreateClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid cluster request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	// Real path: the provider adapter applies Cluster/<Kind>Cluster/
	// KubeadmControlPlane/pools on the management cluster, then fleet
	// registers the record it returns. Falls back to in-memory registration
	// when the provider isn't configured (mirrors pool provisioning).
	if p, err := providers.Get(req.Provider.Kind); err == nil && p.Ready() {
		if cp, ok := p.(providers.ClusterProvisioner); ok {
			cluster, err := cp.ProvisionCluster(r.Context(), req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			s.fleet.RegisterCluster(*cluster)
			writeJSON(w, http.StatusCreated, *cluster)
			return
		}
	}
	c := s.fleet.CreateCluster(req)
	writeJSON(w, http.StatusCreated, c)
}

// ListNodePoolsHandler mirrors NVKE GET /nodePools.
func (s *Server) ListNodePoolsHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"nodePools": s.fleet.NodePools()})
}

// CreateNodePoolHandler mirrors NVKE POST /nodePools. For htmx form posts it
// returns the new nodes' cards to append into the grid; for API clients it
// returns the created NodePool JSON.
func (s *Server) CreateNodePoolHandler(w http.ResponseWriter, r *http.Request) {
	var spec api.NodePoolCreateSpec

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			http.Error(w, "invalid node pool request", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		count, _ := strconv.Atoi(r.FormValue("count"))
		spec = api.NodePoolCreateSpec{
			Name:             r.FormValue("name"),
			NodeNamePrefix:   r.FormValue("name"),
			DesiredNodeCount: max(1, min(count, 8)),
			GPUModel:         r.FormValue("gpu_model"),
			Provider:         api.InfraProvider{Kind: r.FormValue("provider")},
		}
	}

	if spec.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	p, err := providers.Get(spec.Provider.Kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := p.ProvisionNodePool(r.Context(), spec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	res.Pool.ClusterID = "cluster-gpu-fleet"
	s.fleet.AddNodePool("cluster-gpu-fleet", res.Pool)

	if isHTMX(r) {
		var buf bytes.Buffer
		for _, ns := range res.Pool.NodeSets {
			for _, n := range ns.Nodes {
				if n.GPU != nil {
					web.GPUCard(n).Render(r.Context(), &buf)
				}
			}
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(buf.Bytes())
		return
	}
	writeJSON(w, http.StatusCreated, res.Pool)
}

// ListNodesHandler mirrors NVKE GET /nodes.
func (s *Server) ListNodesHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"nodes": s.fleet.AllNodes()})
}

// DrainNodeHandler marks the node Draining and returns the updated card so
// htmx can swap it in place (JSON for API callers). The production path runs
// through the provider adapter: taint -> cordon -> drain -> CAPI replacement.
func (s *Server) DrainNodeHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := s.fleet.Drain(id)
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	// Real path: ask the node's provider adapter to delete/drain the
	// underlying Machine (CAPI handles cordon+drain+replacement). The mock
	// adapter resolves to "mock"; unconfigured adapters fail soft — fleet
	// state already reflects the drain.
	if p, err := providers.Get(strings.ToLower(node.Provider)); err == nil && p.Ready() {
		if err := p.DrainNode(r.Context(), id); err != nil {
			log.Printf("provider drain for node %s: %v", id, err)
		}
	}
	if isHTMX(r) {
		templ.Handler(web.GPUCard(node)).ServeHTTP(w, r)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// ListReleasesHandler mirrors NVKE GET /releases — pinned node releases.
func (s *Server) ListReleasesHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"releases": s.fleet.Releases()})
}


func (s *Server) ClustersPageHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.ClustersPage(s.fleet.Clusters(), s.brand)).ServeHTTP(w, r)
}

func (s *Server) NodePoolsPageHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.NodePoolsPage(s.fleet.NodePools(), s.brand)).ServeHTTP(w, r)
}

func (s *Server) NodesPageHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.NodesPage(s.fleet.AllNodes(), s.brand)).ServeHTTP(w, r)
}

func (s *Server) ReleasesPageHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.ReleasesPage(s.fleet.Releases(), s.brand)).ServeHTTP(w, r)
}

func (s *Server) InferencePageHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.InferencePage(s.fleet.Endpoints(), s.fleet.NodePools(), s.brand)).ServeHTTP(w, r)
}

func (s *Server) ProvidersPageHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.ProvidersPage(providers.List(), s.brand)).ServeHTTP(w, r)
}

func (s *Server) SettingsPageHandler(w http.ResponseWriter, r *http.Request) {
	vars := map[string]string{
		"BRAND_NAME": s.brand.Name, "BRAND_LOGO": s.brand.Logo,
		"BRAND_TAGLINE": s.brand.Tagline, "BRAND_STATUS": s.brand.Status,
		"BRAND_THEME": s.brand.Theme,
	}
	env := map[string]string{}
	for k := range vars {
		env[k] = "env: " + k
	}
	templ.Handler(web.SettingsPage(s.brand, vars, env, s.brand.Theme)).ServeHTTP(w, r)
}

// gpuHourlyRate estimates list price per GPU-hour by provider, used until
// real billing telemetry lands.
var gpuHourlyRate = map[string]float64{
	"aws": 4.10, "gcp": 3.67, "azure": 3.40, "coreweave": 2.21,
	"metal3": 0.0, "nico": 0.0, "mock": 0.0, "multi": 0.0,
}

func (s *Server) FinOpsPageHandler(w http.ResponseWriter, r *http.Request) {
	var rows []web.FinRow
	var total float64
	for _, p := range s.fleet.NodePools() {
		gpus := 0
		var model string
		for _, ns := range p.NodeSets {
			for _, n := range ns.Nodes {
				if n.GPU != nil {
					gpus++
					model = n.GPU.Model
				}
			}
		}
		rate := gpuHourlyRate[p.Provider.Kind]
		cost := rate * float64(gpus) * 24
		total += cost
		rows = append(rows, web.FinRow{
			Label: p.Name, Provider: p.Provider.Kind, GPUs: gpus,
			Model: model, RateHr: rate, CostDay: cost,
		})
	}
	templ.Handler(web.FinOpsPage(rows, total, s.brand)).ServeHTTP(w, r)
}

// CreateInferenceHandler registers a model endpoint on a pool. htmx gets the
// new table row; JSON callers get the endpoint object.
func (s *Server) CreateInferenceHandler(w http.ResponseWriter, r *http.Request) {
	var e api.InferenceEndpoint
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, "invalid endpoint request", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		e = api.InferenceEndpoint{
			Name:   r.FormValue("name"),
			Model:  r.FormValue("model"),
			Engine: r.FormValue("engine"),
			PoolID: r.FormValue("pool"),
		}
	}
	if e.Name == "" || e.Model == "" {
		http.Error(w, "name and model are required", http.StatusBadRequest)
		return
	}
	if e.Engine == "" {
		e.Engine = "vllm"
	}
	if e.Replicas < 1 {
		e.Replicas = 1
	}
	e.ID = "ep-" + e.Name
	e.Status = "Ready"
	e.CreatedAt = time.Now()
	s.fleet.AddEndpoint(e)

	if isHTMX(r) {
		templ.Handler(web.EndpointRow(e)).ServeHTTP(w, r)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) ProvisionModalHandler(w http.ResponseWriter, r *http.Request) {
	templ.Handler(web.ProvisionModal(providers.List())).ServeHTTP(w, r)
}

func (s *Server) CloseModalHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
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
			if _, err := w.Write([]byte("event: gpu-metrics\ndata: " + payload + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// runTelemetry mutates fleet metrics on a tick and broadcasts each updated
// GPU node as an OOB card fragment. Milestone 3 replaces MutateMetrics with
// real DCGM scrapes.
func (s *Server) runTelemetry(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fleet.MutateMetrics()
			for _, node := range s.fleet.GPUNodes() {
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
