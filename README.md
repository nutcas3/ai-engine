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
| `BRAND_THEME` | light | initial theme: `light`, `dark`, or `brutalist` |

Example: `BRAND_NAME="Acme GPU Cloud" BRAND_LOGO=AC ./main`

## CI, releases, and container images

GitHub Actions runs `make verify` on pushes and pull requests. Releases use
[Conventional Commits](https://www.conventionalcommits.org/): pushes to
`main` maintain a Release Please PR with the next semantic version and
changelog. Merge that PR to create the `vX.Y.Z` tag and publish:

- Cross-platform Linux, macOS, and Windows binaries for amd64 and arm64,
  plus `checksums.txt`, attached to the GitHub Release.
- A multi-architecture container image published to
  `ghcr.io/nutcas3/ai-engine:<version>` and `:latest`.

Examples: `feat: add cluster autoscaling` triggers a minor release;
`fix: handle empty telemetry` triggers a patch release; include `BREAKING
CHANGE:` in the commit body for a major release. Other commit types (such as
`docs:` and `chore:`) do not trigger a version bump.

No PAT or extra release secret is needed. The workflows use the repository's
`GITHUB_TOKEN`. In repository Settings → Actions → General, allow GitHub
Actions to create pull requests so Release Please can open its version PR.
To make the GHCR package public, set its visibility to Public once under the
repository owner's Packages settings; subsequent images publish automatically.

## Install & run

```sh
# install the tool
go install github.com/nutcas3/ai-engine/cmd/api@latest
# or download a release binary / docker image ghcr.io/nutcas3/ai-engine

# run the control plane + dashboard (default :8090)
ai-engine
# then open http://localhost:8090
# durable mode: DATABASE_URL=postgres://... ; auth: OIDC_ISSUER + OIDC_AUDIENCE
```


