# Cluster API Providers

The control plane treats every infrastructure target as a CAPI provider. Only
the infrastructure template changes per cloud; `Cluster`/`MachineDeployment`
definitions stay identical.

| Key       | Provider | Install                                        |
|-----------|----------|------------------------------------------------|
| `aws`     | CAPA     | `clusterctl init --infrastructure aws`         |
| `gcp`     | CAPG     | `clusterctl init --infrastructure gcp`         |
| `azure`   | CAPZ     | `clusterctl init --infrastructure azure`       |
| `metal3`  | CAPM3    | `clusterctl init --infrastructure metal3`      |
| `nico`    | CAPNICo  | NVKE management plane (BlueField DPU sites)    |
| `coreweave` | custom | provider-specific CAPI/Terraform adapter       |

## Prerequisites

- `clusterctl` CLI and a bootstrap/management cluster (kind works for dev)
- Cloud credentials per provider:
  - AWS: `clusterawsadm bootstrap iam create-cloudformation-stack`, then
    `AWS_B64ENCODED_CREDENTIALS`
  - GCP: `GCP_B64ENCODED_CREDENTIALS` (base64 service-account key)
  - Metal3: enrolled `BareMetalHost` objects (BMC/Redfish access)
- cert-manager installed before `clusterctl init`

## Useful CAPI features for this engine

- `MachineHealthCheck` + machine taint propagation → node remediation

## Control-plane integration

`internal/providers/capi.go` resolves the management cluster via
`rest.InClusterConfig()`, then `CAPI_KUBECONFIG` → `KUBECONFIG` →
`~/.kube/config`. `CAPI_NAMESPACE` selects where objects land (default
`default`). All six adapters share the client; none report `Ready()` until
it resolves.

`POST /api/nodePools` requires `provider.config.clusterName`; the adapter
upserts `KubeadmConfigTemplate`, `<Kind>MachineTemplate`
(`AWSMachineTemplate`/`GCPMachineTemplate`/`AzureMachineTemplate`/
`Metal3MachineTemplate`/`NICoMachineTemplate`, overridable via
`templateKind`/`templateResource` config keys), and `MachineDeployment`
across `v1beta2`/`v1beta1`. Every other `config` key lands inside the infra
template spec (`instanceType`, `region`, `zone`, `imageID`, `hostSelector`…).
`POST /api/nodes/{id}/drain` deletes the backing `Machine` — CAPI performs
cordon/drain and the `MachineDeployment` creates the replacement.
- In-place updates (`InPlaceUpdates` gate) → driver/firmware updates
- Chained upgrades via ClusterClass → Kubernetes version management
