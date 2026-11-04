# Cilium — uniform CNI across all providers

Same CNI on every target cluster gives identical eBPF networking,
WireGuard/ IPSec encryption, Hubble observability, and NetworkPolicy
semantics on AWS, GCP, and bare metal — no per-cloud CNI quirks.

## Install (per workload cluster, post-CAPI bootstrap)

```sh
cilium install --version stable \
  --set kubeProxyReplacement=true \
  --set encryption.enabled=true --set encryption.type=wireguard \
  --set hubble.enabled=true --set hubble.relay.enabled=true --set hubble.ui.enabled=true
```

For EKS variants set `--set egressMasqueradeInterfaces=eth0` and follow the
Cilium EKS guide (VPC CNI chaining is not needed with full kube-proxy
replacement). On bare metal, pair with Cilium BGP/L2 announcements for
service LoadBalancer IPs instead of a cloud LB.
