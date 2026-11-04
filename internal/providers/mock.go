package providers

import (
	"context"
	"fmt"

	"ai-engine/internal/fleet"
)

// Mock simulates the CAPI lifecycle locally: provisioning immediately adds a
// Ready node to the fleet so the UI can be exercised without a cluster.
type Mock struct {
	key     string
	display string
	ready   bool
	fleet   *fleet.Manager
}

// NewMock returns a Mock bound to the fleet so provisioning shows up in the UI.
func NewMock(f *fleet.Manager) *Mock {
	return &Mock{key: "mock", display: "Mock (local dev)", ready: true, fleet: f}
}

func (m *Mock) Key() string     { return m.key }
func (m *Mock) Display() string { return m.display }
func (m *Mock) Ready() bool     { return m.ready }

func (m *Mock) Provision(_ context.Context, spec NodePoolSpec) error {
	if m.fleet == nil {
		return ErrNotConfigured
	}
	for i := 0; i < spec.Count; i++ {
		m.fleet.Add(fleet.GPUNode{
			ID:          fmt.Sprintf("%s-%d", spec.Name, i),
			Name:        fmt.Sprintf("%s-%02d", spec.Name, i+1),
			Provider:    "Mock",
			GPUModel:    spec.GPUModel,
			VRAMTotalGB: 80.0,
			VRAMUsedGB:  8.0,
			Utilization: 10,
			TempC:       40,
			Status:      "Ready",
		})
	}
	return nil
}

func (m *Mock) Drain(_ context.Context, nodeID string) error {
	if m.fleet == nil {
		return ErrNotConfigured
	}
	_, ok := m.fleet.Drain(nodeID)
	if !ok {
		return fmt.Errorf("node %q not found", nodeID)
	}
	return nil
}
