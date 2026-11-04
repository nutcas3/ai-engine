# LiteLLM — virtual keys, budgets, model routing

Sits in front of the InferencePool endpoints: issues per-team virtual keys,
enforces spend budgets and rate limits, and can fan out to external
model-as-a-service providers for overflow — the FinOps seam.
