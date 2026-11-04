# Architecture

Cloud-agnostic GPU orchestration engine, modeled on NVIDIA NVKE
(`github.com/dsx-ai-factory/nvke`). See `plan.md` for the full strategy and
component versions.

## Three layers

```
+-----------------------------------------------------------------+
|                 1. Unified AI Control Plane                     |
|   Go API · templ+htmx UI · Provider registry · SSE telemetry    |
+-----------------------------------------------------------------+
                                |
+-----------------------------------------------------------------+
|              2. Hardware & Runtime Abstraction Layer            |
|   NVIDIA GPU Operator · DRA · Kueue · KubeRay/LWS · DCGM        |
+-----------------------------------------------------------------+
                                |
+-----------------------------------------------------------------+
|              3. Infrastructure Provider Adapters                |
|   CAPA · CAPG · CAPZ · Metal3 · CAPNICo · custom GPU clouds     |
+-----------------------------------------------------------------+
```

## Package map

| Path | Role |
|---|---|
| `cmd/api` | Entry point, graceful shutdown |
| `cmd/web` | templ views (`base`, `dashboard`, `gpu_card`), embedded assets (htmx + SSE ext, Tailwind v4 output) |
| `internal/server` | HTTP routes, htmx handlers, SSE stream, telemetry tick |
| `internal/fleet` | In-memory `Manager` of `GPUNode` state |
| `internal/providers` | `Provider` interface + registry; `Mock` (local dev), `CAPI` stubs |
| `internal/telemetry` | SSE `Broadcaster`; DCGM Xid→action classification table, scraper stub |
| `deploy/` | Per-layer install manifests (CAPI, GPU Operator, Kueue, Cilium, Gateway API inference, Fluid) |

## Data flow

- **Dashboard** — `GET /` renders the fleet grid; `hx-ext="sse"` connects to
  `/api/telemetry/stream`. Each tick, `runTelemetry` mutates metrics and
  broadcasts one `GPUCardOOB` fragment per node; htmx patches cards in place
  via `hx-swap-oob`.
- **Drain** — `POST /api/nodes/{id}/drain` → `fleet.Drain` → returns the
  updated card (htmx `outerHTML` swap). Production path: provider adapter →
  taint/cordon → drain → CAPI `MachineDeployment` replaces the node.
- **Provision** — modal lists `providers.List()`; only `Ready()` adapters can
  submit. `POST /api/clusters` → `Provider.Provision` → new cards appended to
  `#fleet-grid` via `beforeend`.

## Extension seams

- **New cloud** → implement `providers.Provider`, call `providers.Register`.
- **Real metrics** → implement `telemetry.Scraper` against per-node DCGM
  exporter endpoints; feed `fleet.Manager` instead of `MutateMetrics`.
- **Fault remediation** → `telemetry.Classify(xid)` drives taint → cordon →
  drain → provider `Drain`.
