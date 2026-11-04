# Kubeflow Trainer v2 + JobSet — fine-tuning

`TrainJob` CRDs for gang-scheduled multi-node training, admitted through
Kueue with topology-aware scheduling (TAS). JobSet composes the rank group;
Multus + SR-IOV NetworkAttachmentDefinitions attach RDMA/IB interfaces on
`max` tier. NCCL tests live in `training/examples/` upstream.
