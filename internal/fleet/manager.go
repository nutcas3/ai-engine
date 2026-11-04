package fleet

import (
	"math"
	"math/rand"
	"sync"
)

// GPUNode is the control plane's unified view of a GPU worker, regardless of
// which infrastructure provider (CAPA, CAPG, Metal3, NICo, ...) backs it.
type GPUNode struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`  // AWS, GCP, CoreWeave, On-Prem
	GPUModel    string  `json:"gpu_model"` // NVIDIA H100, A100, L40S
	VRAMUsedGB  float64 `json:"vram_used"`
	VRAMTotalGB float64 `json:"vram_total"`
	Utilization int     `json:"utilization"` // 0-100%
	TempC       int     `json:"temp_c"`
	Status      string  `json:"status"` // Ready, Degraded, Draining
}

// Manager holds fleet state in memory. A real deployment backs this with the
// management cluster's CAPI objects and DCGM telemetry instead.
type Manager struct {
	mu    sync.RWMutex
	nodes []GPUNode
}

func NewManager() *Manager {
	return &Manager{
		nodes: []GPUNode{
			{ID: "node-1", Name: "us-east-h100-01", Provider: "AWS (CAPA)", GPUModel: "NVIDIA H100 80GB", VRAMUsedGB: 64.2, VRAMTotalGB: 80.0, Utilization: 88, TempC: 62, Status: "Ready"},
			{ID: "node-2", Name: "coreweave-a100-04", Provider: "CoreWeave", GPUModel: "NVIDIA A100 80GB", VRAMUsedGB: 78.5, VRAMTotalGB: 80.0, Utilization: 99, TempC: 78, Status: "Degraded"},
			{ID: "node-3", Name: "baremetal-h100-01", Provider: "On-Prem (Metal3)", GPUModel: "NVIDIA H100 80GB", VRAMUsedGB: 22.0, VRAMTotalGB: 80.0, Utilization: 25, TempC: 48, Status: "Ready"},
		},
	}
}

func (m *Manager) Nodes() []GPUNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]GPUNode, len(m.nodes))
	copy(cp, m.nodes)
	return cp
}

func (m *Manager) Drain(id string) (GPUNode, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.nodes {
		if m.nodes[i].ID == id {
			m.nodes[i].Status = "Draining"
			m.nodes[i].VRAMUsedGB = 0
			m.nodes[i].Utilization = 0
			return m.nodes[i], true
		}
	}
	return GPUNode{}, false
}

func (m *Manager) Add(node GPUNode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes = append(m.nodes, node)
}

// MutateMetrics simulates live DCGM telemetry until real metrics are wired in.
func (m *Manager) MutateMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.nodes {
		if m.nodes[i].Status != "Ready" {
			continue
		}
		delta := (rand.Float64() * 4) - 2
		m.nodes[i].VRAMUsedGB = math.Max(10, math.Min(m.nodes[i].VRAMTotalGB, m.nodes[i].VRAMUsedGB+delta))
		m.nodes[i].Utilization = int(math.Max(5, math.Min(100, float64(m.nodes[i].Utilization)+rand.Float64()*10-5)))
	}
}
