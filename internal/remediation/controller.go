// Package remediation closes the self-healing loop: Degraded nodes (set by
// the DCGM scraper on Xid errors) are evaluated against the Xid→action table
// and the worst offender triggers drain → CAPI replacement. On optimal+ tier
// clusters NVSentinel owns this role on-cluster; this controller covers
// nano/minimal tiers and direct remediation.
package remediation

import (
	"context"
	"log"
	"strings"
	"time"

	"ai-engine/internal/fleet"
	"ai-engine/internal/providers"
	"ai-engine/internal/telemetry"
)

// rank orders actions so the worst Xid wins.
var rank = map[telemetry.XidAction]int{
	telemetry.XidIgnore: 0, telemetry.XidReboot: 1,
	telemetry.XidDrainReset: 2, telemetry.XidReplace: 3,
}

type Controller struct {
	fleet *fleet.Manager
	acted map[string]telemetry.XidAction // nodeID → action already taken
}

func New(f *fleet.Manager) *Controller {
	return &Controller{fleet: f, acted: map[string]telemetry.XidAction{}}
}

// Run reconciles Degraded nodes every 5s until ctx is cancelled.
func (c *Controller) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.reconcile(ctx)
		}
	}
}

func (c *Controller) reconcile(ctx context.Context) {
	for _, n := range c.fleet.AllNodes() {
		if n.GPU == nil || n.Status() != "Degraded" || len(n.GPU.XidErrors) == 0 {
			continue
		}
		// Worst action across the node's Xid codes.
		worst, code := telemetry.XidIgnore, 0
		for _, x := range n.GPU.XidErrors {
			if a := telemetry.Classify(x); rank[a] > rank[worst] {
				worst, code = a, x
			}
		}
		if rank[worst] < rank[telemetry.XidDrainReset] {
			continue // informational only
		}
		if prev, ok := c.acted[n.ID]; ok && rank[prev] >= rank[worst] {
			continue // already acted at this severity
		}
		c.acted[n.ID] = worst

		// taint+cordon+drain+replace: the provider adapter deletes the
		// Machine (CAPI controller drains + replaces); fleet marks Draining
		// either way so the UI shows the remediation in flight.
		kind := strings.ToLower(n.Provider)
		if p, err := providers.Get(kind); err == nil && p.Ready() {
			if err := p.DrainNode(ctx, n.ID); err != nil {
				log.Printf("remediation: drain %s (xid %d): %v", n.ID, code, err)
				continue
			}
		}
		if _, ok := c.fleet.Drain(n.ID); ok {
			log.Printf("remediation: node %s draining — xid %d → %s", n.ID, code, worst)
		}
	}
}
