# NVSentinel — GPU health remediation

NVIDIA's open-source node-health agent: watches DCGM for Xid errors, ECC
faults, NVLink degradation and thermal events, then drives the remediation
loop (cordon → drain → node replacement via CAPI `MachineHealthCheck`).

This is the production implementation of the `telemetry.Classify` Xid table
in `internal/telemetry/dcgm.go` — deploy NVSentinel on `optimal`+ tiers and
let it own remediation rather than writing a custom controller.

## Install

```sh
helm install nvsentinel oci://ghcr.io/nvidia/nvsentinel/charts/nvsentinel \
  -n nvsentinel --create-namespace
```

Key values: `nodeHealthChecks.dcgm.enabled`, remediation connector
(Kubernetes `NodeCondition` → CAPI machine remediation), syslog/janitor
policies per environment.
