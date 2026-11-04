# Qdrant (+ Milvus on max) — vector search

RAG data plane: Argo Workflows pipelines for ingest→chunk→embed→upsert,
Qdrant for vector search (×1 small tiers, ×3 optimal), Milvus added on
`max`. CloudNativePG backs relational state. See `ai-for-k8s/data/`.
