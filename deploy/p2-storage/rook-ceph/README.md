# Rook-Ceph + RGW — distributed block + S3

`optimal`+ tier storage: Rook operator + CephCluster for block/RWX, Ceph
Object Gateway exposing the standard S3 API the engine assumes everywhere
(model registry, datasets, checkpoints).

`nano`/`minimal` tiers use local-path provisioner + SeaweedFS S3 instead —
see `../seaweedfs/`.
