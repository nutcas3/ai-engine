package providers

import (
	"context"
	"errors"
	"fmt"

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
	Register(&CAPI{key: "aws", display: "AWS — CAPA (EKS/EC2)"})
	Register(&CAPI{key: "gcp", display: "GCP — CAPG (GKE/GCE)"})
	Register(&CAPI{key: "azure", display: "Azure — CAPZ (AKS/VMs)"})
	Register(&CAPI{key: "coreweave", display: "CoreWeave / Lambda (custom CAPI)"})
	Register(&CAPI{key: "metal3", display: "Bare Metal — Metal3 (CAPM3)"})
	Register(&CAPI{key: "nico", display: "Bare Metal — NICo (BlueField DPU sites)"})
}
