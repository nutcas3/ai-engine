# ai-engine

Cloud-agnostic AI engine control plane — a single Go binary serving a
templ + htmx dashboard for GPU fleet telemetry, node remediation, and
multi-cloud node-pool provisioning. Modeled on NVIDIA NVKE.

See[docs/architecture.md](docs/architecture.md) for the package map.

## Stack

- **Go** stdlib `net/http` — control-plane API
- **templ** — type-safe HTML components compiled to Go
- **htmx 2.x + SSE ext** — live metric swaps, no JS framework
- **Tailwind CSS v4** — standalone CLI, CSS-first config

## Run

```sh
make build    # templ generate + tailwind + go build -o main
PORT=8090 ./main
```

or live-reload dev loop: `make watch` (requires `air`).

Open `http://localhost:8090` — three seeded GPU nodes (AWS/CAPA,
CoreWeave, on-prem Metal3) stream metrics over SSE every 2s. The Degraded
node exposes a drain action that swaps its card to `Draining (CAPI)`.
`+ Provision Node Pool` opens the provider modal; only the Mock provider
provisions until CAPI adapters are configured.

## Endpoints

| Route | Purpose |
|---|---|
| `GET /` | Fleet dashboard |
| `GET /health` | Liveness |
| `GET /api/telemetry/stream` | SSE `gpu-metrics` stream (OOB card fragments) |
| `POST /api/nodes/{id}/drain` | Drain node → returns updated card |
| `GET /api/clusters/provision` | Provision modal |
| `POST /api/clusters` | Provision node pool via provider adapter |

## Layout

```
cmd/api            entry point
cmd/web            templ views + embedded assets (htmx, sse.js, output.css)
internal/fleet     in-memory GPUNode state
internal/providers Provider interface + registry (mock, CAPI stubs)
internal/telemetry SSE broadcaster + DCGM/Xid classification
internal/server    routes, handlers, telemetry loop
deploy/            manifests: clusterapi, gpu-operator, kueue, cilium,
                   gateway-api (inference ext), fluid weight caching
docs/              architecture.md
```

Generated with [go-blueprint](https://github.com/Melkeydev/go-blueprint)
(stdlib backend, htmx, tailwind, docker, githubaction), then extended with
the engine layers.

## White-labeling

The control plane rebrands via env vars (see `.env`):

| Var | Default | Where it appears |
|---|---|---|
| `BRAND_NAME` | AI Engine Control Plane | nav title + `<title>` |
| `BRAND_LOGO` | AI | logo square |
| `BRAND_TAGLINE` | cloud·agnostic | chip next to the name |
| `BRAND_STATUS` | Control Plane Active | mint status pill |

Example: `BRAND_NAME="Acme GPU Cloud" BRAND_LOGO=AC ./main`
