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
- In-place updates (`InPlaceUpdates` gate) → driver/firmware updates
- Chained upgrades via ClusterClass → Kubernetes version management
