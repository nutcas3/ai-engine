package providers

import (
	"context"
	"errors"
	"fmt"
)

// NodePoolSpec describes a GPU node pool the control plane should provision.
// Provider-specific details (instance type, VPC, image) are carried in Labels
// until real CAPI templates are wired in.
type NodePoolSpec struct {
	Name     string
	Provider string // registry key: aws, gcp, azure, coreweave, metal3, nico
	GPUModel string
	Count    int
	Labels   map[string]string
}

// Provider is the seam between the control plane and an infrastructure
// adapter (Cluster API provider, bare-metal controller, or cloud API).
type Provider interface {
	// Key is the registry identifier (e.g. "aws", "metal3").
	Key() string
	// Display is a human-readable name for the UI.
	Display() string
	// Ready reports whether the adapter is configured for real provisioning.
	Ready() bool
	// Provision asks the adapter to create a node pool from spec.
	Provision(ctx context.Context, spec NodePoolSpec) error
	// Drain cordons and drains a node, triggering replacement.
	Drain(ctx context.Context, nodeID string) error
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
