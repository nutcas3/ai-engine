// Package api holds the control-plane domain model, mirroring NVIDIA NVKE's
// resource hierarchy (controller/api/openapi.yaml):
//
//	Cluster -> NodePool -> NodeSet -> Node
//
// with provider payloads fixed at creation and status carried as conditions.
// GPU telemetry fields are this engine's addition on top of the NVKE shape.
package api

import "time"

// Condition mirrors metav1.Condition as used across NVKE resources.
type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"` // True, False, Unknown
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

// InfraProvider is the one-of provider payload fixed at creation. It
// generalizes NVKE's nico|launchLayer|infra union to any adapter registered
// in internal/providers.
type InfraProvider struct {
	Kind   string            `json:"kind"`             // aws|gcp|azure|coreweave|metal3|nico|mock
	Config map[string]string `json:"config,omitempty"` // provider-specific identifiers
}

// ClusterNetwork holds the cluster pod/service CIDRs.
type ClusterNetwork struct {
	PodCIDR     string `json:"podCIDR"`
	ServiceCIDR string `json:"serviceCIDR"`
}

// Cluster is a managed Kubernetes cluster — NVKE's top-level resource.
// Tier selects the deploy/clusters/<tier> GitOps profile (nano, minimal,
// optimal, max) that decides which platform layers sync onto the cluster.
type Cluster struct {
	ID                            string          `json:"id"`
	Name                          string          `json:"name"`
	Tier                          string          `json:"tier"`
	ControlPlaneKubernetesVersion string          `json:"controlPlaneKubernetesVersion"`
	CNIPlugin                     string          `json:"cniPlugin"`
	EtcdEncrypted                 bool            `json:"etcdEncrypted"`
	Network                       ClusterNetwork  `json:"network"`
	Provider                      InfraProvider   `json:"provider"`
	NodePools                     []NodePool      `json:"nodePools,omitempty"`
	Conditions                    []Condition     `json:"conditions"`
	CreatedAt                     time.Time       `json:"createdAt"`
}

// CreateClusterRequest mirrors NVKE CreateClusterRequest.
type CreateClusterRequest struct {
	Name                          string               `json:"name"`
	Tier                          string               `json:"tier,omitempty"` // nano|minimal|optimal|max; default optimal
	ControlPlaneKubernetesVersion string               `json:"controlPlaneKubernetesVersion"`
	EtcdEncrypted                 bool                 `json:"etcdEncrypted"`
	Provider                      InfraProvider        `json:"provider"`
	NodePools                     []NodePoolCreateSpec `json:"nodePools,omitempty"`
}

// NodePool is a pool of worker nodes sharing config — NVKE /nodePools.
type NodePool struct {
	ID                string        `json:"id"`
	ClusterID         string        `json:"clusterID"`
	Name              string        `json:"name"`
	NodeNamePrefix    string        `json:"nodeNamePrefix"`
	KubernetesVersion string        `json:"kubernetesVersion"`
	DesiredNodeCount  int           `json:"desiredNodeCount"`
	Provider          InfraProvider `json:"provider"`
	NodeSets          []NodeSet     `json:"nodeSets,omitempty"`
	Conditions        []Condition   `json:"conditions"`
}

// NodePoolCreateSpec mirrors NVKE NodePoolCreateConfig.
type NodePoolCreateSpec struct {
	Name              string        `json:"name"`
	NodeNamePrefix    string        `json:"nodeNamePrefix"`
	DesiredNodeCount  int           `json:"desiredNodeCount"`
	KubernetesVersion string        `json:"kubernetesVersion,omitempty"`
	GPUModel          string        `json:"gpuModel,omitempty"`
	Provider          InfraProvider `json:"provider"`
}

// NodeSet is a homogeneous group inside a pool — NVKE /nodeSets. A pool can
// hold multiple sets (e.g. across releases or AZs).
type NodeSet struct {
	ID                string        `json:"id"`
	NodePoolID        string        `json:"nodePoolID"`
	DesiredCount      int           `json:"desiredCount"`
	KubernetesVersion string        `json:"kubernetesVersion"`
	Provider          InfraProvider `json:"provider"`
	Nodes             []Node        `json:"nodes,omitempty"`
}

// GPUStatus carries per-node accelerator telemetry — this engine's extension
// beyond the NVKE model (fed by DCGM in production).
type GPUStatus struct {
	Model       string  `json:"model"`
	VRAMUsedGB  float64 `json:"vramUsedGB"`
	VRAMTotalGB float64 `json:"vramTotalGB"`
	Utilization int     `json:"utilization"` // 0-100%
	TempC       int     `json:"tempC"`
	XidErrors   []int   `json:"xidErrors,omitempty"`
}

// Node is a single machine — NVKE /nodes.
type Node struct {
	ID           string      `json:"id"`
	NodePoolID   string      `json:"nodePoolID"`
	NodeSetID    string      `json:"nodeSetID"`
	Name         string      `json:"name"`
	IP           string      `json:"ip,omitempty"`
	ControlPlane bool        `json:"controlPlane"`
	Provider     string      `json:"provider"` // display label, denormalized for the UI
	GPU          *GPUStatus  `json:"gpu,omitempty"`
	Conditions   []Condition `json:"conditions"`
}

// Status collapses conditions into the card's single status label.
func (n Node) Status() string {
	for _, c := range n.Conditions {
		switch {
		case c.Type == "Draining" && c.Status == "True":
			return "Draining"
		case c.Type == "Degraded" && c.Status == "True":
			return "Degraded"
		case c.Type == "Ready" && c.Status == "True":
			return "Ready"
		}
	}
	return "Unknown"
}

// InferenceEndpoint is a deployed model-serving endpoint — a vLLM/Triton or
// KServe LLMInferenceService bound to a node pool.
type InferenceEndpoint struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	Engine    string    `json:"engine"` // vllm | triton | kserve | kubeai
	PoolID    string    `json:"poolID"`
	Replicas  int       `json:"replicas"`
	Status    string    `json:"status"` // Ready | Provisioning | Failed
	CreatedAt time.Time `json:"createdAt"`
}

// Release pins the software bundle for a node pool or control plane — NVKE
// /releases (NodePoolRelease): Kubernetes version plus image/component pins.
type Release struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	KubernetesVersion string            `json:"kubernetesVersion"`
	Components        map[string]string `json:"components"` // osImage, gpuDriver, toolkit, dcgm...
}
