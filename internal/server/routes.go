package server

import (
	"bytes"
	"fmt"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"

	"github.com/nutcas3/ai-engine/cmd/web"
	"github.com/nutcas3/ai-engine/internal/api"
	"github.com/nutcas3/ai-engine/internal/auth"
	"github.com/nutcas3/ai-engine/internal/errs"
	"github.com/nutcas3/ai-engine/internal/providers"
	"github.com/nutcas3/ai-engine/internal/telemetry"
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
	mux.HandleFunc("DELETE /api/clusters/{id}", s.DeleteClusterHandler)
	mux.HandleFunc("POST /api/clusters", s.CreateClusterHandler)
	mux.HandleFunc("GET /api/nodePools", s.ListNodePoolsHandler)
	mux.HandleFunc("POST /api/nodePools/{id}/scale", s.ScaleNodePoolHandler)
	mux.HandleFunc("POST /api/nodePools", s.CreateNodePoolHandler)
	mux.HandleFunc("GET /api/nodes", s.ListNodesHandler)
	mux.HandleFunc("POST /api/nodes/{id}/drain", s.DrainNodeHandler)
	mux.HandleFunc("GET /api/releases", s.ListReleasesHandler)
	mux.HandleFunc("DELETE /api/inference/{id}", s.DeleteEndpointHandler)
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
		errs.Write(w, errs.E("clusters.create", errs.Invalid, err, "invalid cluster request"))
		return
	}
	if req.Name == "" {
		errs.Write(w, errs.E("validate", errs.Invalid, errs.ErrInvalid, "name is required"))
		return
	}
	// Durable path: write the record + a workflow_outbox row in one
	// transaction; the processor drives provisioning asynchronously —
	// this handler never calls the orchestrator directly.
	if s.store != nil {
		c := s.fleet.CreateCluster(req)
		job := map[string]any{"type": "cluster.provision", "cluster": req}
		if err := s.store.Enqueue(r.Context(), "cluster", c.ID, c, job); err != nil {
			errs.Write(w, errs.E("enqueue", errs.Internal, err))
			return
		}
		writeJSON(w, http.StatusCreated, c)
		return
	}
	// In-memory path (dev): provision synchronously through the adapter.
	if p, err := providers.Get(req.Provider.Kind); err == nil && p.Ready() {
		if cp, ok := p.(providers.ClusterProvisioner); ok {
			cluster, err := cp.ProvisionCluster(r.Context(), req)
			if err != nil {
				errs.Write(w, errs.E("provision", errs.Provider, err))
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
			errs.Write(w, errs.E("nodepools.create", errs.Invalid, err, "invalid node pool request"))
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			errs.Write(w, errs.E("form", errs.Invalid, err, "bad form"))
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
		errs.Write(w, errs.E("validate", errs.Invalid, errs.ErrInvalid, "name is required"))
		return
	}
	// Durable path: record + outbox row in one transaction; the processor
	// provisions asynchronously and fleet picks it up when it lands.
	if s.store != nil {
		pool := api.NodePool{
			ID: "pool-" + spec.Name, Name: spec.Name,
			DesiredNodeCount: spec.DesiredNodeCount, Provider: spec.Provider,
			Conditions: []api.Condition{{Type: "Provisioning", Status: "True",
				Reason: "Queued", LastTransitionTime: time.Now()}},
		}
		job := map[string]any{"type": "nodepool.provision", "nodePool": spec}
		if err := s.store.Enqueue(r.Context(), "nodepool", pool.ID, pool, job); err != nil {
			errs.Write(w, errs.E("enqueue", errs.Internal, err))
			return
		}
		writeJSON(w, http.StatusAccepted, pool)
		return
	}
	p, err := providers.Get(spec.Provider.Kind)
	if err != nil {
		errs.Write(w, errs.E("create", errs.Invalid, err))
		return
	}
	res, err := p.ProvisionNodePool(r.Context(), spec)
	if err != nil {
		errs.Write(w, errs.E("provision", errs.Provider, err))
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
		errs.Write(w, errs.E("nodes.drain", errs.NotFound, errs.ErrNotFound, "node not found"))
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
			errs.Write(w, errs.E("inference.create", errs.Invalid, err, "invalid endpoint request"))
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			errs.Write(w, errs.E("form", errs.Invalid, err, "bad form"))
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
		errs.Write(w, errs.E("inference.create", errs.Invalid, errs.ErrInvalid, "name and model are required"))
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

	// Real serving path: apply LLMInferenceService + InferencePool +
	// LocalQueue via the pool's provider adapter when configured.
	if pool, ok := s.fleet.PoolByID(e.PoolID); ok {
		kind := "mock"
		if p, ok2 := s.fleet.ClusterByID(pool.ClusterID); ok2 {
			kind = strings.ToLower(p.Provider.Kind)
		}
		if pr, err := providers.Get(kind); err == nil && pr.Ready() {
			if ep, ok3 := pr.(*providers.CAPI); ok3 {
				if err := ep.ProvisionEndpoint(r.Context(), e, auth.TenantFrom(r.Context())); err != nil {
					log.Printf("provision endpoint %s: %v", e.ID, err)
					e.Status = "ProvisioningFailed"
				}
			}
		}
	}

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
		errs.Write(w, errs.E("telemetry.stream", errs.Internal, errs.ErrInvalid, "streaming unsupported"))
		return
	}

	ch := s.broadcaster.Subscribe()
	defer s.broadcaster.Unsubscribe(ch)

	// SSE is long-lived — clear the server's WriteTimeout for this response
	// so the stream isn't killed at 30s.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

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

// runTelemetry scrapes each node's DCGM exporter on a tick (nodes with an
// IP), falls back to the synthetic mutation only for IP-less dev nodes, and
// broadcasts each updated GPU card as an OOB fragment.
func (s *Server) runTelemetry(ctx context.Context) {
	scraper := telemetry.NewScraper()
	lastSeen := map[string]string{}
	bufPool := sync.Pool{New: func() any { return new(bytes.Buffer) }}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Scrape nodes concurrently — each exporter has its own timeout.
			var wg sync.WaitGroup
			for _, node := range s.fleet.GPUNodes() {
				if node.IP == "" {
					continue
				}
				wg.Add(1)
				go func(n api.Node) {
					defer wg.Done()
					if gpu, err := scraper.ScrapeNode(ctx, n); err == nil {
						s.fleet.SetGPU(n.ID, gpu)
					}
				}(node)
			}
			wg.Wait()
			s.fleet.MutateMetrics() // dev fixture: only mutates nodes without IPs

			// Delta broadcast: only re-render + publish cards that changed.
			for _, node := range s.fleet.GPUNodes() {
				fp := fingerprint(node)
				if fp == lastSeen[node.ID] {
					continue
				}
				lastSeen[node.ID] = fp
				buf := bufPool.Get().(*bytes.Buffer)
				buf.Reset()
				if err := web.GPUCardOOB(node).Render(ctx, buf); err != nil {
					bufPool.Put(buf)
					log.Printf("render gpu card: %v", err)
					continue
				}
				s.broadcaster.Publish(buf.String())
				bufPool.Put(buf)
			}
		}
	}
}

// fingerprint is a cheap change signature for delta broadcasts.
func fingerprint(n api.Node) string {
	if n.GPU == nil {
		return n.Status()
	}
	return fmt.Sprintf("%s|%.3f|%d|%d|%v", n.Status(), n.GPU.VRAMUsedGB,
		n.GPU.Utilization, n.GPU.TempC, n.GPU.XidErrors)
}

// DeleteClusterHandler removes a cluster from the fleet and deletes its CAPI
// Cluster (cascades) when a provider is configured.
func (s *Server) DeleteClusterHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cl, ok := s.fleet.ClusterByID(id)
	if !ok {
		errs.Write(w, errs.E("lookup", errs.NotFound, errs.ErrNotFound))
		return
	}
	if p, err := providers.Get(strings.ToLower(cl.Provider.Kind)); err == nil && p.Ready() {
		if cp, ok := p.(*providers.CAPI); ok {
			if err := cp.DeleteCluster(r.Context(), cl.Name, "default"); err != nil {
				log.Printf("delete cluster %s on provider: %v", id, err)
			}
		}
	}
	s.fleet.RemoveCluster(id)
	w.WriteHeader(http.StatusNoContent)
}

// ScaleNodePoolHandler updates a pool's desired count and patches the
// MachineDeployment replicas when a provider is configured.
func (s *Server) ScaleNodePoolHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		DesiredNodeCount int `json:"desiredNodeCount"`
	}
	if err := r.ParseForm(); err == nil && r.FormValue("desiredNodeCount") != "" {
		req.DesiredNodeCount, _ = strconv.Atoi(r.FormValue("desiredNodeCount"))
	} else {
		json.NewDecoder(r.Body).Decode(&req)
	}
	pool, ok := s.fleet.PoolByID(id)
	if !ok {
		errs.Write(w, errs.E("lookup", errs.NotFound, errs.ErrNotFound))
		return
	}
	if cl, ok := s.fleet.ClusterByID(pool.ClusterID); ok {
		if p, err := providers.Get(strings.ToLower(cl.Provider.Kind)); err == nil && p.Ready() {
			if cp, ok2 := p.(*providers.CAPI); ok2 {
				if err := cp.ScaleNodePool(r.Context(), pool.Name, "default", int64(req.DesiredNodeCount)); err != nil {
					log.Printf("scale pool %s: %v", id, err)
				}
			}
		}
	}
	s.fleet.ScalePool(id, req.DesiredNodeCount)
	writeJSON(w, http.StatusOK, map[string]any{"desiredNodeCount": req.DesiredNodeCount})
}

// DeleteEndpointHandler removes an inference endpoint.
func (s *Server) DeleteEndpointHandler(w http.ResponseWriter, r *http.Request) {
	if !s.fleet.RemoveEndpoint(r.PathValue("id")) {
		errs.Write(w, errs.E("lookup", errs.NotFound, errs.ErrNotFound))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
