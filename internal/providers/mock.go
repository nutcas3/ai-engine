package providers

import (
	"context"
	"fmt"
	"time"

	"ai-engine/internal/api"
	"ai-engine/internal/fleet"
)

// Mock simulates the CAPI lifecycle locally: provisioning immediately returns
// a fully-formed NodePool (one NodeSet, desired nodes) so the UI can be
// exercised without a cluster.
type Mock struct {
	key     string
	display string
	fleet   *fleet.Manager
}

// NewMock returns a Mock bound to the fleet so drain calls reach node state.
func NewMock(f *fleet.Manager) *Mock {
	return &Mock{key: "mock", display: "Mock (local dev)", fleet: f}
}

func (m *Mock) Key() string              { return m.key }
func (m *Mock) Display() string          { return m.display }
func (m *Mock) Ready() bool              { return true }
func (m *Mock) MissingConfig() []string  { return nil }

func (m *Mock) ProvisionNodePool(_ context.Context, spec api.NodePoolCreateSpec) (*ProvisionResult, error) {
	if spec.DesiredNodeCount < 1 {
		spec.DesiredNodeCount = 1
	}
	k8s := spec.KubernetesVersion
	if k8s == "" {
		k8s = "1.33.4"
	}
	model := spec.GPUModel
	if model == "" {
		model = "NVIDIA H100 80GB"
	}
	prefix := spec.NodeNamePrefix
	if prefix == "" && spec.Name != "" {
		prefix = spec.Name[:1]
	}

	ready := []api.Condition{{Type: "Ready", Status: "True", LastTransitionTime: time.Now()}}
	nodes := make([]api.Node, 0, spec.DesiredNodeCount)
	for i := 0; i < spec.DesiredNodeCount; i++ {
		nodes = append(nodes, api.Node{
			ID: fmt.Sprintf("%s-%d", spec.Name, i), NodePoolID: "pool-" + spec.Name, NodeSetID: "pool-" + spec.Name + "-set-0",
			Name: fmt.Sprintf("%s-%02d", prefix, i+1), Provider: "Mock", Conditions: ready,
			GPU: &api.GPUStatus{Model: model, VRAMTotalGB: 80, VRAMUsedGB: 8, Utilization: 10, TempC: 40},
		})
	}

	return &ProvisionResult{Pool: api.NodePool{
		ID: "pool-" + spec.Name, Name: spec.Name, NodeNamePrefix: prefix,
		KubernetesVersion: k8s, DesiredNodeCount: spec.DesiredNodeCount,
		Provider:   api.InfraProvider{Kind: m.key},
		Conditions: ready,
		NodeSets: []api.NodeSet{{
			ID: "pool-" + spec.Name + "-set-0", NodePoolID: "pool-" + spec.Name,
			DesiredCount: spec.DesiredNodeCount, KubernetesVersion: k8s,
			Provider: api.InfraProvider{Kind: m.key},
			Nodes:    nodes,
		}},
	}}, nil
}

// ProvisionCluster satisfies ClusterProvisioner for the mock: it mirrors the
// CAPI create flow entirely in memory (cluster + optional pools).
func (m *Mock) ProvisionCluster(ctx context.Context, req api.CreateClusterRequest) (*api.Cluster, error) {
	tier := req.Tier
	if tier == "" {
		tier = "optimal"
	}
	pools := make([]api.NodePool, 0, len(req.NodePools))
	for _, spec := range req.NodePools {
		spec.Provider = req.Provider
		res, err := m.ProvisionNodePool(ctx, spec)
		if err != nil {
			return nil, err
		}
		pools = append(pools, res.Pool)
	}
	return &api.Cluster{
		ID: "cluster-" + req.Name, Name: req.Name, Tier: tier,
		ControlPlaneKubernetesVersion: req.ControlPlaneKubernetesVersion,
		CNIPlugin: "cilium", EtcdEncrypted: req.EtcdEncrypted,
		Provider: req.Provider, NodePools: pools,
		Conditions: []api.Condition{{Type: "Ready", Status: "True", LastTransitionTime: time.Now()}},
		CreatedAt:  time.Now(),
	}, nil
}

func (m *Mock) DrainNode(_ context.Context, nodeID string) error {
	if m.fleet == nil {
		return ErrNotConfigured
	}
	if _, ok := m.fleet.Drain(nodeID); !ok {
		return fmt.Errorf("node %q not found", nodeID)
	}
	return nil
}
