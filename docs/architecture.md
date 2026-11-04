# Architecture

Cloud-agnostic GPU orchestration engine. The domain model and REST surface
follow NVIDIA NVKE (`github.com/dsx-ai-factory/nvke`,
`controller/api/openapi.yaml`). See `plan.md` for strategy and versions.

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

## Domain model (NVKE-aligned)

```
Cluster ── provider payload fixed at creation (InfraProvider oneof)
 └── NodePool ── nodeNamePrefix, kubernetesVersion, desiredNodeCount
      └── NodeSet ── homogeneous group (release/AZ boundary)
           └── Node ── conditions[]; GPU *GPUStatus (our extension)
Release ── pinned component bundle (k8s ver, osImage, driver, toolkit, DCGM)
```

`internal/api/types.go` mirrors the NVKE shapes: `Cluster`,
`CreateClusterRequest`, `NodePool`, `NodePoolCreateSpec`, `NodeSet`, `Node`,
`Condition`, `InfraProvider` (generalizes `nico|launchLayer|infra`),
`Release` (NodePoolRelease). `Node.Status()` collapses conditions into the
UI's Ready/Degraded/Draining label.

## REST surface (NVKE-shaped)

| Route | NVKE analog | Notes |
|---|---|---|
| `GET/POST /api/clusters` | `/clusters` | provider fixed at create |
| `GET/POST /api/nodePools` | `/nodePools` | htmx posts get card HTML back |
| `GET /api/nodes` | `/nodes` | flattened across pools |
| `POST /api/nodes/{id}/drain` | node delete/replace | taint→cordon→drain→CAPI |
| `GET /api/releases` | `/releases` | pinned component bundles |
| `GET /api/telemetry/stream` | — | our SSE addition |
| `GET /api/clusters/provision` | — | UI modal |

Not yet mirrored: `/nodeSets`, `/control-plane`, `/configProfiles`,
`/spire-server` — stubs for later milestones.

## Package map

| Path | Role |
|---|---|
| `cmd/api` | Entry point, graceful shutdown |
| `cmd/web` | templ views (`base`, `dashboard`, `gpu_card`), embedded assets (htmx + SSE ext, Tailwind v4 output) |
| `internal/api` | NVKE-shaped domain types |
| `internal/server` | HTTP routes, htmx handlers, SSE stream, telemetry tick |
| `internal/fleet` | In-memory `Manager` of the cluster→pool→set→node hierarchy |
| `internal/providers` | `Provider` interface + registry; `Mock` (local dev), `CAPI` stubs |
| `internal/telemetry` | SSE `Broadcaster`; DCGM Xid→action classification table, scraper stub |
| `deploy/` | Per-layer install manifests (CAPI, GPU Operator, Kueue, Cilium, Gateway API inference, Fluid) |

## NVKE patterns to adopt in later milestones

- **Outbox pattern** — handlers write the record *and* a `workflow_outbox`
  row in one transaction; a processor starts the provisioning workflow.
  Never call the orchestrator from a handler. (NVKE uses Temporal+Postgres;
  a lighter queue works too.)
- **Node identity** — SPIFFE/SPIRE mTLS with TPM attestation before join
  credentials (`agent/` + `os-image/`).
- **Releases** — `NodePoolRelease` pins the node image and component
  versions per pool; upgrades roll releases through node sets.
- **Authn/Authz** — OIDC JWT authn; tenant-scoped authz before serving API.
- **API-first spec** — NVKE treats `openapi.yaml` as source of truth with
  oapi-codegen output; adopt when the REST surface stabilizes.
