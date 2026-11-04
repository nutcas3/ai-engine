# Envoy Gateway — cluster ingress

Gateway API implementation installed uniformly on every workload cluster.
The `InferencePool` extension (P3) plugs its Endpoint Picker into Envoy via
ext-proc for model-aware LLM routing.

```sh
helm install envoy-gateway oci://docker.io/envoyproxy/gateway-helm \
  -n envoy-gateway-system --create-namespace
```

Pair with Cilium L2 announcements/BGP on bare metal for LoadBalancer IPs
(there is no cloud LB outside the public clouds).
