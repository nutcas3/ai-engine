# Cloud-Agnostic AI Engine — Architecture & Implementation Plan

A single control plane that orchestrates GPU workloads across public clouds (AWS, GCP, Azure), specialized GPU clouds (CoreWeave, Lambda, RunPod), and on-premise bare metal — without vendor lock-in. Modeled on NVIDIA's open-source NVKE (`github.com/dsx-ai-factory/nvke`, Apache 2.0, currently preview).

> Component versions verified November 2026. See "Current Versions" at the bottom.

## Architecture: Three Layers

```
+-----------------------------------------------------------------+
|                 1. Unified AI Control Plane                     |
|      (Cluster API, Declarative CRDs, nvkectl-style CLI)         |
+-----------------------------------------------------------------+
                                |
+-----------------------------------------------------------------+
|              2. Hardware & Runtime Abstraction Layer            |
|   (NVIDIA GPU Operator, DRA, KubeRay, Kueue, DCGM telemetry)    |
+-----------------------------------------------------------------+
                                |
+-----------------------------------------------------------------+
|              3. Infrastructure Provider Adapters                |
|  +----------------+  +----------------+  +-------------------+  |
|  | CAPI-AWS (EKS) |  | CAPI-GCP (GKE) |  | Bare-Metal (NICo) |  |
|  +----------------+  +----------------+  +-------------------+  |
+-----------------------------------------------------------------+
```

## Key Architectural Strategies

### 1. Standardize Infrastructure Provisioning with Cluster API (CAPI)

Don't write native SDK integrations per cloud (EC2, GCE, Azure VMs). Build around CAPI's declarative resources (`Cluster`, `MachineSet`, `MachineDeployment`) and swap infrastructure providers:

- **CAPA** — AWS (EC2 self-managed or EKS variant)
- **CAPG** — GCP (Compute Engine or GKE)
- **CAPZ** — Azure (VMs or AKS)
- **CAPM3** (Metal3, now CNCF incubating) / **CAPT** (Tinkerbell) — generic bare metal via BMC/Redfish/PXE
- **CAPNICo** — NVKE's provider for NVIDIA Infra Controller; requires BlueField DPUs, so it suits AI-factory-scale sites, not generic bare metal

Only the infrastructure template changes per provider (`AWSMachineTemplate` vs `Metal3MachineTemplate`); the cluster definition stays identical.

Recent CAPI features that help this design:
- **In-place updates** (`InPlaceUpdates` gate) — update machines without delete/recreate
- **Chained upgrades** — skip-version upgrades via ClusterClass runtime extensions
- **Machine taint propagation** (`MachineTaintPropagation` gate) — taints on Machines flow to Nodes, useful for the remediation loop

### 2. Abstract GPU Resources via Kubernetes Operators

Never rely on provider-specific GPU setups (e.g., GKE auto-provisioning). Install standard operators uniformly on every target:

- **NVIDIA GPU Operator** (v26.7.x) — drivers, container toolkit, MIG, identical on AWS `p4d`, CoreWeave VM, or on-prem H100. Bundles DCGM exporter 4.8.x and the DRA driver v0.5.x.
- **Kubernetes DRA** — **GA since K8s 1.34, locked-on since 1.35**. `ResourceClaim`-based GPU selection by attributes (memory, compute capability, topology) via CEL; MIG and multi-node NVLink `ComputeDomain`s. K8s 1.37 added **DRA extended-resource GA** (legacy `nvidia.com/gpu` requests transparently served by DRA — enables gradual migration) and **device taints GA** (driver-level tainting of unhealthy devices, complementing the remediation loop). DRA is now the default path; the classic device plugin remains as fallback for older clusters.
- **DCGM exporter** — per-node telemetry (VRAM, utilization, temp, Xid errors, ECC)

### 3. Hardware Self-Healing Remediation Loop

Confirmed real-world pattern (EKS node monitoring agent, NVKE auto-mode, gpu-node-guardian):

1. DCGM exporter detects Xid error / thermal fault → Prometheus metrics
2. Controller scrapes DCGM per node, classifies Xid (79 = fell off bus → replace; 74 = NVLink error → replace; 48 = double-bit ECC → drain + reset; 94 = contained app error)
3. Taint + cordon node (optionally taint the DRA device too, now GA in 1.37), evict affected workloads
4. Drain; CAPI `MachineDeployment` provisions a clean replacement automatically

### 4. Standardize Storage & Model Weight Caching

- **Block/state**: cloud-agnostic CSI (Rook/Ceph, Longhorn) or per-cloud CSI behind a uniform StorageClass interface
- **Object storage**: standardize on S3 API (MinIO or Ceph Object Gateway) across environments
- **Weight caching**: Fluid + Alluxio (or JuiceFS) — pull multi-GB checkpoints from S3 to node-local NVMe. Colocating a single-node cache with the vLLM pod cuts 70B-class cold starts to ~3 min (vs ~18 min cold pull). Dragonfly P2P is the option for large fan-out.

### 5. Decouple Networking & Inference Ingress

- **Cilium** as the uniform CNI across all clouds — eBPF networking, WireGuard encryption, consistent NetworkPolicy
- **Gateway API Inference Extension** for model serving: `InferencePool` + `InferenceModel` CRDs with an Endpoint Picker (Envoy ext-proc) that routes by queue depth, KV-cache state, and GPU memory — the standard approach; supported by Istio, kgateway, Envoy Gateway, GKE Gateway

### 6. Serving & Scheduling Stack

- **vLLM** (OpenAI-compatible) and **Triton/Dynamo-Triton** wrapped in standard Helm charts
- **LeaderWorkerSet (LWS)** — now the standard API for multi-node inference (leader + worker pods as one unit of scaling/rollout/topology); its new sibling **DisaggregatedSet (DS)** handles prefill/decode-disaggregated serving with autoscaling and failure handling. Preferred over raw KubeRay for multi-host vLLM.
- **KubeRay** — still the right choice for Ray-native distributed training jobs and Ray Serve
- **Kueue** — the queueing/quota layer that makes multi-tenancy work: `ClusterQueue`/`LocalQueue` GPU quotas per team, gang scheduling for RayJob/LWS/JobSet, priority preemption, topology-aware placement, **multi-cluster dispatch (MultiKueue)** — which is exactly the mechanism for bursting jobs across clouds behind one control plane. Native DRA `ResourceClaim` support.
- HPA on inference metrics (TTFT, queue depth) not just CPU/memory

## Management UI Stack

**Go + templ + htmx** — single compiled binary, type-safe templates, no JS framework:

- `templ` (v0.3.x) compiles `.templ` → Go functions; template errors fail at build time
- **htmx 4** (`hx-sse` extension for Server-Sent Events) — released Aug 2026, fetch-based rewrite. Still on npm `next` tag; htmx 2.x remains `latest` until early 2027. Use 4.0.x for new builds (`upgrade-check` CLI flags migration issues if needed); fall back to 2.0.x + `ext/sse.js` only for maximum stability.
- SSE pushes GPU metric updates; htmx swaps only the affected DOM nodes
- Tailwind CSS (CDN for dev, compiled for production)

### UI modules

1. **Cluster & node pool management** — multi-cloud provisioner, GPU capacity matrix (VRAM, MIG, health)
2. **Workload & model studio** — vLLM/Triton endpoints, LWS groups, replicas, TTFT-based autoscaling; Kueue queue depth; KubeRay training jobs
3. **Hardware faults & remediation** — DCGM/Xid feed, drain/replace triggers
4. **FinOps** — cost per token by model/team/provider, spot arbitrage hints

## Planned Repository Layout

```
ai-engine/
├── plan.md                        # this file
├── README.md
├── go.mod                         # module ai-engine, dep: github.com/a-h/templ
├── Makefile                       # dev / build / generate / clean
├── cmd/server/main.go             # entry point, mux wiring
├── internal/
│   ├── fleet/manager.go           # thread-safe node state, metrics, drain
│   ├── providers/
│   │   ├── provider.go            # Provider interface + registry
│   │   ├── mock.go                # local-dev provider simulating CAPI lifecycle
│   │   └── capi.go                # CAPI adapter stub (dynamic client — creds TODO)
│   ├── telemetry/
│   │   ├── broadcaster.go         # SSE fan-out, newline-safe framing
│   │   └── dcgm.go                # DCGM scrape stub + Xid classification table
│   └── api/handlers.go            # dashboard, drain, provision modal, telemetry stream
├── views/
│   ├── layout.templ               # htmx 4 + hx-sse + Tailwind
│   ├── dashboard.templ            # fleet grid + provision modal anchor
│   └── components/gpu_card.templ  # status badge, VRAM bar, drain button
├── deploy/
│   ├── clusterapi/                # clusterctl init notes per provider
│   ├── gpu-operator/values.yaml   # GPU Operator + DCGM exporter Helm values
│   ├── kueue/                     # ClusterQueue/LocalQueue/ResourceFlavor stubs
│   ├── cilium/                    # uniform CNI notes
│   ├── gateway-api/               # InferencePool/InferenceModel pointers
│   └── fluid/dataset.yaml         # Dataset + AlluxioRuntime stub
└── docs/architecture.md
```

## Implementation Roadmap

### Milestone 1 — Control plane foundation
Go backend (stdlib `net/http`), in-memory fleet state, `Provider` interface + mock provider, SSE broadcaster, templ views, drain/provision endpoints.

### Milestone 2 — CAPI integration
`clusterctl` management cluster, provider controllers (CAPA/CAPG/metal3), template generators per provider, async workers for cluster lifecycle; Postgres for state.

### Milestone 3 — GPU operator & telemetry
Auto-install GPU Operator + DCGM exporter + DRA driver on cluster bootstrap; ingest real DCGM metrics into the SSE pipeline; implement the Xid→cordon→drain→replace loop.

### Milestone 4 — Serving, quotas & RBAC
Kueue queues per team (this *is* the multi-tenancy/quota mechanism), vLLM/Triton Helm-based one-click deploys with LWS for multi-node, Gateway API Inference Extension wiring, OIDC auth, FinOps cost views.

## Trade-offs

| Feature | Cloud-native approach | Cloud-agnostic engine |
|---|---|---|
| Vendor lock-in | High | Zero — workloads migrate freely |
| Operational overhead | Low (provider manages control plane) | High (you run the abstraction plane) |
| InfiniBand / RDMA | Cloud-dependent | Needs custom bare-metal adapters |
| Cost optimization | Single provider pricing | Arbitrage GPUs across providers (Kueue multi-cluster) |

## Current Versions (verified Nov 2026)

| Component | Version / Status |
|---|---|
| Kubernetes | ~1.37/1.38; DRA GA since 1.34, extended-resource DRA GA in 1.37 |
| Cluster API | v1.13.x stable (v1.15.0-beta out); in-place updates, chained upgrades, machine taint propagation |
| NVIDIA GPU Operator | v26.7.2; bundles DCGM exporter 4.8.4, DRA driver v0.5.1, device plugin v0.20.x |
| htmx | 4.0.1 (`next` tag; 2.0.x remains `latest` until early 2027); SSE via `hx-sse` in v4 |
| templ | v0.3.1100 |
| NVKE / NICo | Preview; Apache 2.0; NICo needs BlueField DPUs |
| Metal3 | CNCF incubating — the generic bare-metal CAPI provider |
| Kueue | v0.1x stable line; gang scheduling for KubeRay/LWS/JobSet, MultiKueue cross-cluster dispatch |
| LWS / DisaggregatedSet | Stable docs; standard for multi-node + disaggregated inference |

## Known Caveats

- **NICo needs BlueField DPUs** — use Metal3/Tinkerbell for generic bare metal; NICo only for NVIDIA DPU-equipped AI-factory sites.
- **NVIDIA DRA driver GPU features** are still maturing even though core DRA is GA — enable per-cluster, keep device plugin fallback.
- **SSE framing**: rendered templ HTML contains newlines that corrupt `data:` fields — strip newlines or emit one `data:` line per HTML line.
- **htmx 4 vs 2.x**: attributes changed in v4 (`hx-sse:*` syntax, explicit attribute inheritance). Pin a version; don't mix docs across majors.
- Tailwind/htmx CDN links are dev-only; ship compiled assets for production.
