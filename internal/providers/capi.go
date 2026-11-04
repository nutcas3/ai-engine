package providers

import (
	"context"

	"ai-engine/internal/api"
)

// CAPI is a placeholder adapter for Cluster API infrastructure providers
// (CAPA, CAPG, CAPZ, Metal3, NICo). Milestone 2 wires it to the management
// cluster via a dynamic client that applies Cluster/MachineDeployment objects.
type CAPI struct {
	key     string
	display string
}

func (c *CAPI) Key() string     { return c.key }
func (c *CAPI) Display() string { return c.display }
func (c *CAPI) Ready() bool     { return false }

func (c *CAPI) ProvisionNodePool(_ context.Context, _ api.NodePoolCreateSpec) (*ProvisionResult, error) {
	return nil, ErrNotConfigured
}

func (c *CAPI) DrainNode(_ context.Context, _ string) error {
	return ErrNotConfigured
}
