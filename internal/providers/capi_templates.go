package providers

import (
	"ai-engine/internal/api"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// unstructuredObj wraps an unstructured.Unstructured so templates can carry
// their API version independently of the applied version.
type unstructuredObj struct {
	obj *unstructured.Unstructured
}

func newObj(kind, name, ns string, spec map[string]interface{}) *unstructuredObj {
	return &unstructuredObj{obj: &unstructured.Unstructured{Object: map[string]interface{}{
		"kind":     kind,
		"metadata": map[string]interface{}{"name": name, "namespace": ns},
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
	case "nico":
		return "nicomachinetemplates"
	default:
		return kind + "machinetemplates"
	}
}

// infraTemplate builds the provider infra MachineTemplate object. Spec
// fields come from spec.Provider.Config so operators can pass real cloud
// parameters (instanceType, region, zone, imageID, hostSelector, …) through
// the create request without the adapter hardcoding them.
func (c *CAPI) infraTemplate(spec api.NodePoolCreateSpec, ns string) (*unstructuredObj, error) {
	inner := map[string]interface{}{}
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
		map[string]interface{}{"template": map[string]interface{}{"spec": inner}}), nil
}

// kubeadmConfigTemplate builds the bootstrap config for pool machines.
func kubeadmConfigTemplate(spec api.NodePoolCreateSpec, ns, k8s string) *unstructuredObj {
	return newObj("KubeadmConfigTemplate", spec.Name+"-kubeadm", ns, map[string]interface{}{
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"joinConfiguration": map[string]interface{}{
					"nodeRegistration": map[string]interface{}{
						"kubeletExtraArgs": map[string]interface{}{
							"cloud-provider": "external",
						},
					},
				},
			},
		},
	})
}

// machineDeployment builds the CAPI MachineDeployment wiring the infra
// template + kubeadm bootstrap to the target cluster.
func machineDeployment(spec api.NodePoolCreateSpec, ns, clusterName string, replicas int, k8s, infraKind string) *unstructuredObj {
	labels := map[string]interface{}{
		"cluster.x-k8s.io/cluster-name": clusterName,
		"cluster.x-k8s.io/deployment-name": spec.Name,
		"pool": spec.Name,
	}
	return newObj("MachineDeployment", spec.Name, ns, map[string]interface{}{
		"clusterName": clusterName,
		"replicas":    int64(replicas),
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"pool": spec.Name},
		},
		"template": map[string]interface{}{
			"metadata": map[string]interface{}{"labels": labels},
			"spec": map[string]interface{}{
				"clusterName": clusterName,
				"version":     k8s,
				"bootstrap": map[string]interface{}{
					"configRef": map[string]interface{}{
						"apiVersion": "bootstrap.cluster.x-k8s.io/v1beta1",
						"kind":       "KubeadmConfigTemplate",
						"name":       spec.Name + "-kubeadm",
						"namespace":  ns,
					},
				},
				"infrastructureRef": map[string]interface{}{
					"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta1",
					"kind":       infraKind,
					"name":       spec.Name + "-mt",
					"namespace":  ns,
				},
			},
		},
	})
}

