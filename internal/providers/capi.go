package providers

import "context"

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

func (c *CAPI) Provision(_ context.Context, _ NodePoolSpec) error {
	return ErrNotConfigured
}

func (c *CAPI) Drain(_ context.Context, _ string) error {
	return ErrNotConfigured
}
