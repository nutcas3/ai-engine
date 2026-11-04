// Package fleet holds control-plane state as an NVKE-shaped hierarchy:
// Cluster -> NodePool -> NodeSet -> Node, with GPU telemetry on nodes.
// A real deployment backs this with Postgres (outbox pattern, per NVKE's
// controller) plus the management cluster's CAPI objects and DCGM telemetry.
package fleet

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"ai-engine/internal/api"
)

// Manager holds the cluster hierarchy in memory.
type Manager struct {
	mu        sync.RWMutex
	clusters  []api.Cluster
	releases  []api.Release
	endpoints []api.InferenceEndpoint
}

// NewManager seeds one fleet cluster with three provider-backed pools, so the
// dashboard exercises multi-cloud state out of the box.
func NewManager() *Manager {
	ready := []api.Condition{{Type: "Ready", Status: "True", LastTransitionTime: time.Now()}}
	degraded := []api.Condition{{Type: "Degraded", Status: "True", Reason: "NvidiaXID79Error", LastTransitionTime: time.Now()}}

	pool := func(id, name, provider, model string, conds []api.Condition, node api.Node) api.NodePool {
		return api.NodePool{
			ID: id, ClusterID: "cluster-gpu-fleet", Name: name, NodeNamePrefix: name[:1],
			KubernetesVersion: "1.33.4", DesiredNodeCount: 1,
			Provider:   api.InfraProvider{Kind: provider},
			Conditions: conds,
			NodeSets: []api.NodeSet{{
				ID: id + "-set-0", NodePoolID: id, DesiredCount: 1,
				KubernetesVersion: "1.33.4",
				Provider:          api.InfraProvider{Kind: provider},
				Nodes:             []api.Node{node},
			}},
		}
	}

	gpuNode := func(id, poolID, name, providerLabel, model string, used, total float64, util, temp int, conds []api.Condition, xids []int) api.Node {
		return api.Node{
			ID: id, NodePoolID: poolID, NodeSetID: poolID + "-set-0",
			Name: name, Provider: providerLabel, Conditions: conds,
			GPU: &api.GPUStatus{
				Model: model, VRAMUsedGB: used, VRAMTotalGB: total,
				Utilization: util, TempC: temp, XidErrors: xids,
			},
		}
	}

	return &Manager{
		clusters: []api.Cluster{{
			ID: "cluster-gpu-fleet", Name: "gpu-fleet", Tier: "optimal",
			ControlPlaneKubernetesVersion: "1.33.4",
			CNIPlugin:                     "cilium",
			EtcdEncrypted:                 true,
			Network:                       api.ClusterNetwork{PodCIDR: "10.244.0.0/16", ServiceCIDR: "10.96.0.0/12"},
			Provider:                      api.InfraProvider{Kind: "multi"},
			Conditions:                    ready,
			CreatedAt:                     time.Now(),
			NodePools: []api.NodePool{
				pool("pool-aws", "aws-h100-pool", "aws", "h100", ready,
					gpuNode("node-1", "pool-aws", "us-east-h100-01", "AWS (CAPA)", "NVIDIA H100 80GB", 64.2, 80, 88, 62, ready, nil)),
				pool("pool-cw", "coreweave-a100-pool", "coreweave", "a100", degraded,
					gpuNode("node-2", "pool-cw", "coreweave-a100-04", "CoreWeave", "NVIDIA A100 80GB", 78.5, 80, 99, 78, degraded, []int{79})),
				pool("pool-bm", "metal3-h100-pool", "metal3", "h100", ready,
					gpuNode("node-3", "pool-bm", "baremetal-h100-01", "On-Prem (Metal3)", "NVIDIA H100 80GB", 22, 80, 25, 48, ready, nil)),
			},
		}},
		releases: []api.Release{{
			ID: "rel-1.33.4", Name: "nodepoolrelease-v1.33.4",
			KubernetesVersion: "1.33.4",
			Components: map[string]string{
				"osImage": "ubuntu-24.04-nvidia-2026.10", "gpuDriver": "595.91.07",
				"containerToolkit": "1.20.1", "dcgmExporter": "4.8.4", "draDriver": "0.5.1",
			},
		}},
		endpoints: []api.InferenceEndpoint{{
			ID: "ep-llama-chat", Name: "llama-chat", Model: "llama-3-70b-chat",
			Engine: "vllm", PoolID: "pool-aws", Replicas: 2,
			Status: "Ready", CreatedAt: time.Now(),
		}},
	}
}

// Clusters returns a copy of the cluster hierarchy.
func (m *Manager) Clusters() []api.Cluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]api.Cluster, len(m.clusters))
	copy(cp, m.clusters)
	return cp
}

// Endpoints returns the registered inference endpoints.
func (m *Manager) Endpoints() []api.InferenceEndpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]api.InferenceEndpoint, len(m.endpoints))
	copy(cp, m.endpoints)
	return cp
}

// AddEndpoint registers a new inference endpoint.
func (m *Manager) AddEndpoint(e api.InferenceEndpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endpoints = append(m.endpoints, e)
}

// PoolByID returns the pool matching id, for endpoint detail joins.
func (m *Manager) PoolByID(id string) (api.NodePool, bool) {
	for _, p := range m.NodePools() {
		if p.ID == id || p.Name == id {
			return p, true
		}
	}
	return api.NodePool{}, false
}

// Releases returns the pinned release list.
func (m *Manager) Releases() []api.Release {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]api.Release, len(m.releases))
	copy(cp, m.releases)
	return cp
}

// NodePools flattens all pools across clusters.
func (m *Manager) NodePools() []api.NodePool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []api.NodePool
	for _, c := range m.clusters {
		out = append(out, c.NodePools...)
	}
	return out
}

// AllNodes flattens every node across pools/sets.
func (m *Manager) AllNodes() []api.Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []api.Node
	for _, c := range m.clusters {
		for _, p := range c.NodePools {
			for _, s := range p.NodeSets {
				out = append(out, s.Nodes...)
			}
		}
	}
	return out
}

// GPUNodes returns only nodes carrying accelerator telemetry — the set the
// dashboard renders.
func (m *Manager) GPUNodes() []api.Node {
	var out []api.Node
	for _, n := range m.AllNodes() {
		if n.GPU != nil {
			out = append(out, n)
		}
	}
	return out
}

// eachNode calls fn with a pointer to every node in the hierarchy under write
// lock, so mutations persist.
func (m *Manager) eachNode(fn func(n *api.Node)) {
	for ci := range m.clusters {
		for pi := range m.clusters[ci].NodePools {
			for si := range m.clusters[ci].NodePools[pi].NodeSets {
				for ni := range m.clusters[ci].NodePools[pi].NodeSets[si].Nodes {
					fn(&m.clusters[ci].NodePools[pi].NodeSets[si].Nodes[ni])
				}
			}
		}
	}
}

// Drain sets the Draining condition and zeros in-flight GPU usage, mirroring
// the cordon+drain step before CAPI replaces the node.
func (m *Manager) Drain(id string) (api.Node, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out api.Node
	found := false
	m.eachNode(func(n *api.Node) {
		if n.ID != id {
			return
		}
		n.Conditions = []api.Condition{{Type: "Draining", Status: "True", LastTransitionTime: time.Now()}}
		if n.GPU != nil {
			n.GPU.VRAMUsedGB = 0
			n.GPU.Utilization = 0
		}
		out, found = *n, true
	})
	return out, found
}

// AddNodePool appends a provisioned pool (one node set, DesiredNodeCount
// nodes) to the named cluster.
func (m *Manager) AddNodePool(clusterID string, p api.NodePool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ci := range m.clusters {
		if m.clusters[ci].ID == clusterID || m.clusters[ci].Name == clusterID {
			m.clusters[ci].NodePools = append(m.clusters[ci].NodePools, p)
			return
		}
	}
}

// RegisterCluster appends a cluster built by a provider adapter (e.g. CAPI
// ProvisionCluster) so it appears alongside seeded/requested clusters.
func (m *Manager) RegisterCluster(c api.Cluster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters = append(m.clusters, c)
}

// CreateCluster registers a new cluster from a create request.
func (m *Manager) CreateCluster(req api.CreateClusterRequest) api.Cluster {
	m.mu.Lock()
	defer m.mu.Unlock()
	tier := req.Tier
	if tier == "" {
		tier = "optimal"
	}
	c := api.Cluster{
		ID:                            "cluster-" + req.Name,
		Name:                          req.Name,
		Tier:                          tier,
		ControlPlaneKubernetesVersion: req.ControlPlaneKubernetesVersion,
		CNIPlugin:                     "cilium",
		EtcdEncrypted:                 req.EtcdEncrypted,
		Provider:                      req.Provider,
		Conditions:                    []api.Condition{{Type: "Provisioning", Status: "True", LastTransitionTime: time.Now()}},
		CreatedAt:                     time.Now(),
	}
	m.clusters = append(m.clusters, c)
	return c
}

// MutateMetrics simulates live DCGM telemetry on Ready GPU nodes until real
// scrapes are wired in.
func (m *Manager) MutateMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eachNode(func(n *api.Node) {
		if n.GPU == nil || n.Status() != "Ready" {
			return
		}
		delta := (rand.Float64() * 4) - 2
		n.GPU.VRAMUsedGB = math.Max(10, math.Min(n.GPU.VRAMTotalGB, n.GPU.VRAMUsedGB+delta))
		n.GPU.Utilization = int(math.Max(5, math.Min(100, float64(n.GPU.Utilization)+rand.Float64()*10-5)))
	})
}

// FindNode returns a copy of the named node.
func (m *Manager) FindNode(id string) (api.Node, bool) {
	for _, n := range m.AllNodes() {
		if n.ID == id {
			return n, true
		}
	}
	return api.Node{}, false
}

// PoolIDForNode maps node ID back to its pool — used by the provision
// response renderer.
func (m *Manager) NodeIDsInPool(poolName string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for _, c := range m.clusters {
		for _, p := range c.NodePools {
			if p.Name != poolName {
				continue
			}
			for _, s := range p.NodeSets {
				for _, n := range s.Nodes {
					ids = append(ids, n.ID)
				}
			}
		}
	}
	return ids
}
