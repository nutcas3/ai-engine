# KServe — model serving CRDs

`LLMInferenceService` is the declarative unit for a vLLM endpoint: model URI
(S3/HF), GPU count, tensor parallelism, router (GIE `InferencePool`),
autoscaling on TTFT/queue depth.

```sh
helm install kserve oci://ghcr.io/kserve/charts/kserve -n kserve --create-namespace
```

Small tiers use **KubeAI** instead (lighter, CPU/GGUF capable,
scale-to-zero) — see `ai-for-k8s/inference/` for the tier split.
