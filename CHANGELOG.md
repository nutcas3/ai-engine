# Changelog

## [1.1.1](https://github.com/nutcas3/ai-engine/compare/ai-engine-v1.1.0...ai-engine-v1.1.1) (2026-11-10)


### Bug Fixes

* move tag_prefix under git section for goreleaser v2 ([377c9cf](https://github.com/nutcas3/ai-engine/commit/377c9cfe7fc1dc08a42c4af984cfa2fb18acd79e))

## [1.1.0](https://github.com/nutcas3/ai-engine/compare/ai-engine-v1.0.0...ai-engine-v1.1.0) (2026-11-08)


### Features

* cluster delete, pool scale, endpoint delete end-to-end ([cc38a1c](https://github.com/nutcas3/ai-engine/commit/cc38a1c0905cfb9a48f094a4f4811c2b10608f6b))
* panic recovery middleware at the HTTP boundary ([c53d4be](https://github.com/nutcas3/ai-engine/commit/c53d4beda1a0f9308fd6c1db33a719fbd128ea32))
* structured error layer (internal/errs) ([2796268](https://github.com/nutcas3/ai-engine/commit/2796268ee063ef7e0e0017e2737127d06af2e6db))


### Bug Fixes

* clear write deadline on SSE stream + slowloris hardening ([f0d6385](https://github.com/nutcas3/ai-engine/commit/f0d6385a4f05554e61af155f3081ad529b59c43a))
* goreleaser tag_prefix for release-please component tags ([d5dbcb4](https://github.com/nutcas3/ai-engine/commit/d5dbcb49bcbbc6ff2756053295866ff916aadd97))
* modernize go ([1f57043](https://github.com/nutcas3/ai-engine/commit/1f57043149a7132694b2b9c8704275adc72e3073))


### Performance Improvements

* pool render buffers, prealloc slices, pprof listener ([399826e](https://github.com/nutcas3/ai-engine/commit/399826edda3b02f462f142c8b0124898cd637ebd))

## 1.0.0 (2026-11-08)


### Features

* Add ArgoCD root-app manifests for all four tiers ([c6f68b5](https://github.com/nutcas3/ai-engine/commit/c6f68b5acc1c5aa48d204e846cb20a5378dc3aed))
* Add cloud-agnostic AI engine control plane ([7aa16f2](https://github.com/nutcas3/ai-engine/commit/7aa16f272e773a1880234807fa0ff5fd9da4ab99))
* Add collapsible icon sidebar; ignore local plan.md ([ac66a75](https://github.com/nutcas3/ai-engine/commit/ac66a751fd569b8867c81997e2d364de2b5335cb))
* Add OIDC JWT/JWKS auth on mutating /api/* ([77bb0d5](https://github.com/nutcas3/ai-engine/commit/77bb0d524264455bc0280516235be65ee0d30bb4))
* Add Postgres durable state and outbox provisioning ([d5fb440](https://github.com/nutcas3/ai-engine/commit/d5fb4405ac1d3d37f666af2a65724889c2dc6a0c))
* Add remediation controller closing the self-healing loop ([f5eefa5](https://github.com/nutcas3/ai-engine/commit/f5eefa5c06265407f8e6fdf86b840da7ed8a1ccb))
* Add self-hosted CAPI adapters (Tinkerbell, vSphere, OpenStack, Proxmox) ([d42c92d](https://github.com/nutcas3/ai-engine/commit/d42c92d62a8c33585636a21bbe3dd79afac383eb))
* Add sidebar navigation and full section routes ([b72b3f6](https://github.com/nutcas3/ai-engine/commit/b72b3f60961c82f0c0b8189b672cfcc6c56c76bb))
* Align domain model and REST surface with NVKE ([7e83ef8](https://github.com/nutcas3/ai-engine/commit/7e83ef8d54ee1e9228aa37e802a6de92283bc09f))
* Apply full [@theme](https://github.com/theme) token set and add white-label branding ([2ac1a68](https://github.com/nutcas3/ai-engine/commit/2ac1a688a15b1c02c16273a2530153c41ca62581))
* Apply full day-0 CAPI objects and route cluster create ([e6b90a0](https://github.com/nutcas3/ai-engine/commit/e6b90a0c367595bc2ca2964c117996af81df0b37))
* Apply GitOps day-0 handoff on cluster create ([9c008e3](https://github.com/nutcas3/ai-engine/commit/9c008e3c0e8daf85995fcc5d468b587e94885baa))
* Auto-install GPU Operator + DRA on cluster bootstrap ([dc564ac](https://github.com/nutcas3/ai-engine/commit/dc564acfa11866adfe0be9f179220ab052af25f9))
* Implement real CAPI adapter with dynamic client ([03eab56](https://github.com/nutcas3/ai-engine/commit/03eab56da272cbe061a7a48c75352a1198fbdad9))
* Implement real DCGM telemetry scraper ([7b07691](https://github.com/nutcas3/ai-engine/commit/7b07691fe727a4528a5d2228b217add0d4dfe5d8))
* initial public release ([c3854a0](https://github.com/nutcas3/ai-engine/commit/c3854a03be41f7da3943eb3118520319aaeca340))
* Integrate ai-for-k8s GitOps layers and cluster tiers ([3022bb2](https://github.com/nutcas3/ai-engine/commit/3022bb2f489800efacb4c7ed751e34fb4407a348))
* Lucide icons, bolder nav strokes, themed white-label, Settings page ([fbd597b](https://github.com/nutcas3/ai-engine/commit/fbd597b30605685518c20806cbb5164ba34475c4))
* Probe CAPI CRDs before marking adapters ready ([ca8072b](https://github.com/nutcas3/ai-engine/commit/ca8072bad086d9b8740e2ccd2c1e77f2df9c2df3))
* Probe provider-specific CRDs for per-adapter readiness ([de9b992](https://github.com/nutcas3/ai-engine/commit/de9b9926dc394e42caf6c66d9400197109fda159))
* Replace map[string]interface{} with map[string]any and simplify node count logic ([b7da039](https://github.com/nutcas3/ai-engine/commit/b7da039b01a9ea2d8a3443765107aa9c133302f1))
* Require provider credentials for Ready(); surface missing config ([776736a](https://github.com/nutcas3/ai-engine/commit/776736abf2bdaafe88ea478a16188ddb83ed8936))
* Restyle UI to brutalist-editorial light theme ([4bf3712](https://github.com/nutcas3/ai-engine/commit/4bf37126e5c0eb5b1353124921e69849846fa89f))
* Switch to Geist light theme and rebuild navbar ([f96500e](https://github.com/nutcas3/ai-engine/commit/f96500e6b322bcc217889e13f082beabf20cc8b7))
* Wire inference endpoints to real serving objects ([c977d8d](https://github.com/nutcas3/ai-engine/commit/c977d8d5bc5ddbbf7a98c1279995008f3a3bcb2c))


### Bug Fixes

* Fix stale Tailwind CSS in dev loop ([22e06c3](https://github.com/nutcas3/ai-engine/commit/22e06c3cbfd37c4b87c58802e1c607fe00e87cee))
