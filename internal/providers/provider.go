package providers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ai-engine/internal/api"
)

// ProvisionResult is what an adapter returns after creating a node pool: the
// pool resource (with its node set and nodes) to register in fleet state.
type ProvisionResult struct {
	Pool api.NodePool
}

// Provider is the seam between the control plane and an infrastructure
// adapter (Cluster API provider, bare-metal controller, or cloud API). It
// mirrors NVKE's model where each pool/set carries exactly one provider
// payload, fixed at creation.
type Provider interface {
	// Key is the registry identifier (e.g. "aws", "metal3").
	Key() string
	// Display is a human-readable name for the UI.
	Display() string
	// Ready reports whether the adapter is configured for real provisioning.
	Ready() bool
	// ProvisionNodePool asks the adapter to create a pool from spec.
	ProvisionNodePool(ctx context.Context, spec api.NodePoolCreateSpec) (*ProvisionResult, error)
	// DrainNode cordons and drains a node, triggering replacement.
	DrainNode(ctx context.Context, nodeID string) error
}

// ClusterProvisioner is the optional extension for providers that can create
// the management-cluster objects for a whole Cluster (Cluster +
// <Kind>Cluster + KubeadmControlPlane + CP MachineTemplate), not just worker
// pools. CAPI implements it; the Mock returns an in-memory cluster so the
// create flow works end-to-end in dev.
type ClusterProvisioner interface {
	ProvisionCluster(ctx context.Context, req api.CreateClusterRequest) (*api.Cluster, error)
}

// ErrNotConfigured marks adapters that exist as placeholders but have no
// credentials/controller wired up yet.
var ErrNotConfigured = errors.New("provider adapter not configured")

var registry = map[string]Provider{}

// Register adds a provider to the registry, keyed by Provider.Key().
func Register(p Provider) {
	registry[p.Key()] = p
}

// Get returns the provider for key, or an error if unregistered.
func Get(key string) (Provider, error) {
	p, ok := registry[key]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", key)
	}
	return p, nil
}

// List returns all registered providers, for populating the provision UI.
func List() []Provider {
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}

func init() {
	// One shared management-cluster client for every CAPI adapter. Each
	// adapter probes independently: core CAPI CRDs + its own infra cluster
	// CRD must be served before it reports Ready().
	dyn, ns, _ := capiClientFromEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, p := range []struct{ key, display string }{
		{"aws", "AWS — CAPA (EKS/EC2)"},
		{"gcp", "GCP — CAPG (GKE/GCE)"},
		{"azure", "Azure — CAPZ (AKS/VMs)"},
		{"coreweave", "CoreWeave / Lambda (custom CAPI)"},
		{"metal3", "Bare Metal — Metal3 (CAPM3)"},
		{"nico", "Bare Metal — NICo (BlueField DPU sites)"},
	} {
		a := NewCAPI(p.key, p.display, dyn, ns, false)
		a.ready = a.probeProvider(ctx)
		Register(a)
	}
}
