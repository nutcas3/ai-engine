package providers

import (
	"github.com/nutcas3/ai-engine/internal/api"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// unstructuredObj wraps an unstructured.Unstructured so templates can carry
// their API version independently of the applied version.
type unstructuredObj struct {
	obj *unstructured.Unstructured
}

func newObj(kind, name, ns string, spec map[string]any) *unstructuredObj {
	return &unstructuredObj{obj: &unstructured.Unstructured{Object: map[string]any{
		"kind":     kind,
		"metadata": map[string]any{"name": name, "namespace": ns},
		"spec":     spec,
	}}}
}

// templateKind picks the infra MachineTemplate kind for this provider. Any
// provider can override via config["templateKind"] (e.g. a custom CAPI
// adapter such as CoreWeave's or NICo objects).
func (c *CAPI) templateKind(cfg map[string]string) string {
	if k := cfg["templateKind"]; k != "" {
		return k
	}
	switch c.key {
	case "aws":
		return "AWSMachineTemplate"
	case "gcp":
		return "GCPMachineTemplate"
	case "azure":
		return "AzureMachineTemplate"
	case "metal3":
		return "Metal3MachineTemplate"
	case "tinkerbell":
		return "TinkerbellMachineTemplate"
	case "vsphere":
		return "VSphereMachineTemplate"
	case "openstack":
		return "OpenStackMachineTemplate"
	case "proxmox":
		return "ProxmoxMachineTemplate"
	case "nico":
		return "NICoMachineTemplate"
	default:
		return c.key + "MachineTemplate"
	}
}

// infraResourceName maps a provider kind to its MachineTemplate resource
// name (lower plural). Overridable via config["templateResource"].
func infraResourceName(kind string, cfg map[string]string) string {
	if r := cfg["templateResource"]; r != "" {
		return r
	}
	switch kind {
	case "aws":
		return "awsmachinetemplates"
	case "gcp":
		return "gcpmachinetemplates"
	case "azure":
		return "azuremachinetemplates"
	case "metal3":
		return "metal3machinetemplates"
	case "tinkerbell":
		return "tinkerbellmachinetemplates"
	case "vsphere":
		return "vspheremachinetemplates"
	case "openstack":
		return "openstackmachinetemplates"
	case "proxmox":
		return "proxmoxmachinetemplates"
	case "nico":
		return "nicomachinetemplates"
	default:
		return kind + "machinetemplates"
	}
}

// infraClusterKind is the infra Cluster kind for the provider (AWSCluster,
// GCPCluster, Metal3Cluster, NICoCluster…). config["clusterKind"] overrides.
func (c *CAPI) infraClusterKind(cfg map[string]string) string {
	if k := cfg["clusterKind"]; k != "" {
		return k
	}
	switch c.key {
	case "aws":
		return "AWSCluster"
	case "gcp":
		return "GCPCluster"
	case "azure":
		return "AzureCluster"
	case "metal3":
		return "Metal3Cluster"
	case "tinkerbell":
		return "TinkerbellCluster"
	case "vsphere":
		return "VSphereCluster"
	case "openstack":
		return "OpenStackCluster"
	case "proxmox":
		return "ProxmoxCluster"
	case "nico":
		return "NICoCluster"
	default:
		return c.key + "Cluster"
	}
}

// infraClusterResourceName maps a provider kind to its Cluster resource.
// config["clusterResource"] overrides.
func infraClusterResourceName(kind string, cfg map[string]string) string {
	if r := cfg["clusterResource"]; r != "" {
		return r
	}
	switch kind {
	case "aws":
		return "awsclusters"
	case "gcp":
		return "gcpclusters"
	case "azure":
		return "azureclusters"
	case "metal3":
		return "metal3clusters"
	case "tinkerbell":
		return "tinkerbellclusters"
	case "vsphere":
		return "vsphereclusters"
	case "openstack":
		return "openstackclusters"
	case "proxmox":
		return "proxmoxclusters"
	case "nico":
		return "nicoclusters"
	default:
		return kind + "clusters"
	}
}

// infraCluster builds the provider <Kind>Cluster object. Spec fields come
// from config minus adapter-reserved keys (region, sshKeyName, zone, …).
func (c *CAPI) infraCluster(name, ns string, cfg map[string]string) *unstructuredObj {
	inner := map[string]any{}
	for k, v := range cfg {
		switch k {
		case "clusterName", "namespace", "templateKind", "templateResource",
			"clusterKind", "clusterResource", "controlPlaneCount",
			"podCIDR", "serviceCIDR", "cniPlugin":
		default:
			inner[k] = v
		}
	}
	return newObj(c.infraClusterKind(cfg), name, ns, inner)
}

// kubeadmControlPlane builds the controlplane.cluster.x-k8s.io object
// referencing the CP MachineTemplate built earlier at infraVer.
func kubeadmControlPlane(name, ns, k8s string, replicas int64, infraKind, infraVer string) *unstructuredObj {
	return newObj("KubeadmControlPlane", name+"-kcp", ns, map[string]any{
		"replicas": replicas,
		"version":  k8s,
		"machineTemplate": map[string]any{
			"infrastructureRef": map[string]any{
				"apiVersion": "infrastructure.cluster.x-k8s.io/" + infraVer,
				"kind":       infraKind,
				"name":       name + "-cp-mt",
				"namespace":  ns,
			},
		},
		"kubeadmConfigSpec": map[string]any{
			"initConfiguration": map[string]any{
				"nodeRegistration": map[string]any{
					"kubeletExtraArgs": map[string]any{"cloud-provider": "external"},
				},
			},
		},
	})
}

// clusterObj builds the top-level cluster.x-k8s.io Cluster wiring the infra
// cluster and KubeadmControlPlane refs at the versions they were applied.
func clusterObj(name, ns, tier, infraClusterKind, kcpVer, infraVer string) *unstructuredObj {
	o := newObj("Cluster", name, ns, map[string]any{
		"infrastructureRef": map[string]any{
			"apiVersion": "infrastructure.cluster.x-k8s.io/" + infraVer,
			"kind":       infraClusterKind,
			"name":       name,
			"namespace":  ns,
		},
		"controlPlaneRef": map[string]any{
			"apiVersion": "controlplane.cluster.x-k8s.io/" + kcpVer,
			"kind":       "KubeadmControlPlane",
			"name":       name + "-kcp",
			"namespace":  ns,
		},
		"clusterNetwork": map[string]any{
			"pods":     map[string]any{"cidrBlocks": []any{"192.168.0.0/16"}},
			"services": map[string]any{"cidrBlocks": []any{"10.96.0.0/12"}},
		},
	})
	o.obj.SetLabels(map[string]string{
		"ai-engine.io/tier":      tier,
		"ai-engine.io/gpu-layer": "enabled",
	})
	return o
}

// gpuOperatorProxy builds an addons.cluster.x-k8s.io HelmChartProxy that
// installs the NVIDIA GPU Operator (driver, device plugin, DRA driver, DCGM
// exporter) onto every workload cluster labeled ai-engine.io/gpu-layer —
// CAPI's native day-1 addon mechanism, so operators ship with bootstrap.
func gpuOperatorProxy(ns string) *unstructuredObj {
	return newObj("HelmChartProxy", "gpu-operator", ns, map[string]interface{}{
		"clusterSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"ai-engine.io/gpu-layer": "enabled"},
		},
		"namespace":   "gpu-operator",
		"releaseName": "gpu-operator",
		"chartName":   "gpu-operator",
		"repoURL":     "https://helm.ngc.nvidia.com/nvidia",
		"version":     "v26.7.2",
		"values": `dcgm-exporter:
  enabled: true
driver:
  enabled: true
devicePlugin:
  enabled: true
draDriver:
  enabled: true
operator:
  defaultRuntime: containerd`,
		"options": map[string]interface{}{
			"wait":                    true,
			"waitForJobsToComplete":  true,
			"maxHistory":             int64(3),
			"disableClusterResourceValidation": true,
			"skipTemplateValidation": true,
		},
	})
}

// infraTemplate builds the provider infra MachineTemplate object. Spec
// fields come from spec.Provider.Config so operators can pass real cloud
// parameters (instanceType, region, zone, imageID, hostSelector, …) through
// the create request without the adapter hardcoding them.
func (c *CAPI) infraTemplate(spec api.NodePoolCreateSpec, ns string) (*unstructuredObj, error) {
	inner := map[string]any{}
	for k, v := range spec.Provider.Config {
		switch k {
		case "clusterName", "namespace", "templateKind", "templateResource":
			// adapter-level keys, not template fields
		default:
			inner[k] = v
		}
	}
	// GPU model hint: pass through as a label-safe instance tag when provided.
	if spec.GPUModel != "" && inner["instanceType"] == nil && inner["machineType"] == nil {
		inner["gpuModel"] = spec.GPUModel
	}
	return newObj(c.templateKind(spec.Provider.Config), spec.Name+"-mt", ns,
		map[string]any{"template": map[string]any{"spec": inner}}), nil
}

// kubeadmConfigTemplate builds the bootstrap config for pool machines.
func kubeadmConfigTemplate(spec api.NodePoolCreateSpec, ns string) *unstructuredObj {
	return newObj("KubeadmConfigTemplate", spec.Name+"-kubeadm", ns, map[string]any{
		"template": map[string]any{
			"spec": map[string]any{
				"joinConfiguration": map[string]any{
					"nodeRegistration": map[string]any{
						"kubeletExtraArgs": map[string]any{
							"cloud-provider": "external",
						},
					},
				},
			},
		},
	})
}

// machineDeployment builds the CAPI MachineDeployment wiring the infra
// template + kubeadm bootstrap to the target cluster. Ref apiVersions use
// the versions those objects were actually applied under.
func machineDeployment(spec api.NodePoolCreateSpec, ns, clusterName string, replicas int, k8s, infraKind, bootVer, infraVer string) *unstructuredObj {
	labels := map[string]any{
		"cluster.x-k8s.io/cluster-name":    clusterName,
		"cluster.x-k8s.io/deployment-name": spec.Name,
		"pool":                             spec.Name,
	}
	return newObj("MachineDeployment", spec.Name, ns, map[string]any{
		"clusterName": clusterName,
		"replicas":    int64(replicas),
		"selector": map[string]any{
			"matchLabels": map[string]any{"pool": spec.Name},
		},
		"template": map[string]any{
			"metadata": map[string]any{"labels": labels},
			"spec": map[string]any{
				"clusterName": clusterName,
				"version":     k8s,
				"bootstrap": map[string]any{
					"configRef": map[string]any{
						"apiVersion": "bootstrap.cluster.x-k8s.io/" + bootVer,
						"kind":       "KubeadmConfigTemplate",
						"name":       spec.Name + "-kubeadm",
						"namespace":  ns,
					},
				},
				"infrastructureRef": map[string]any{
					"apiVersion": "infrastructure.cluster.x-k8s.io/" + infraVer,
					"kind":       infraKind,
					"name":       spec.Name + "-mt",
					"namespace":  ns,
				},
			},
		},
	})
}
