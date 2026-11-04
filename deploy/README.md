# deploy/ — GitOps layer manifests

Layered after the `ai-for-k8s` GitOps repo (Talos + ArgoCD app-of-apps).
The control plane applies these layers to workload clusters in sync-wave
order; each cluster's **tier** (`nano`/`minimal`/`optimal`/`max`) decides
which manifests sync, mirroring `clusters/<tier>/` in that repo.

| Layer | Contents | ai-for-k8s path |
|---|---|---|
| `clusterapi/` | day-0: CAPI providers (CAPA/CAPG/CAPZ/Metal3/NICo) | `bootstrap/` analog |
| `p0-platform/` | Cilium, Envoy Gateway, cert-manager, ESO, Prometheus | `platform/` |
| `p1-gpu/` | NVIDIA GPU Operator, DRA driver, DCGM, NVSentinel | `gpu/` |
| `p2-storage/` | Rook-Ceph + RGW S3 / local-path + SeaweedFS, Fluid | `storage/` |
| `p3-inference/` | GIE (InferencePool/EPP), KServe LLMIsvc, vLLM, LiteLLM | `inference/` |
| `p4-training/` | Kueue (TAS), Kubeflow Trainer v2, JobSet, Multus/SR-IOV | `training/` |
| `p5-data/` | Qdrant, Argo Workflows, CloudNativePG (+Milvus on max) | `data/` |
| `p6-notebooks/` | Z2JH, OIDC, idle culler, Kueue queue | `notebooks/` |
| `clusters/` | tier profiles: which layers/components sync per tier | `clusters/<tier>/` |

## Tiers

| | nano | minimal | optimal | max |
|---|---|---|---|---|
| Nodes | 1, CPU or small GPU | 1–3, ≤4 consumer GPUs | 3+, datacenter GPUs | optimal + NVLink/RDMA |
| GPU mgmt | device plugin | plugin + time-slicing | operator + DRA + NVSentinel + MIG | + ComputeDomains |
| Serving | KubeAI CPU-first | KubeAI scale-to-zero | + GIE, KServe, vLLM, LiteLLM | same |
| Storage | local-path + SeaweedFS | same | Rook-Ceph + RGW | same |
| Training | — | Trainer v2 single-node | + Kueue TAS multi-node | + Multus/SR-IOV/RDMA |
