package providers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ai-engine/internal/api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// capiVersions lists API versions to try in order. CAPI v1.11+ serves
// v1beta2; earlier releases serve v1beta1. Apply tries each until one works.
var capiVersions = []string{"v1beta2", "v1beta1"}

// CAPI is the real Cluster API adapter: it talks to the management cluster
// through a dynamic client and applies Cluster/MachineDeployment/provider
// infra templates per adapter kind (AWSMachineTemplate, GCPMachineTemplate,
// Metal3MachineTemplate, NICo objects).
type CAPI struct {
	key     string
	display string
	dyn     dynamic.Interface
	ns      string
}

// NewCAPI builds a provider for key bound to the management-cluster client.
// A nil dyn means "not configured" — Ready() returns false and provisioning
// fails fast with ErrNotConfigured.
func NewCAPI(key, display string, dyn dynamic.Interface, ns string) *CAPI {
	return &CAPI{key: key, display: display, dyn: dyn, ns: ns}
}

func (c *CAPI) Key() string     { return c.key }
func (c *CAPI) Display() string { return c.display }
func (c *CAPI) Ready() bool     { return c.dyn != nil }

// capiClientFromEnv loads a dynamic client for the CAPI management cluster.
// Resolution order: in-cluster config, CAPI_KUBECONFIG, KUBECONFIG,
// ~/.kube/config. CAPI_NAMESPACE selects the namespace holding CAPI objects
// (default "default").
func capiClientFromEnv() (dynamic.Interface, string, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kc := os.Getenv("CAPI_KUBECONFIG")
		if kc == "" {
			kc = os.Getenv("KUBECONFIG")
		}
		if kc == "" {
			home, herr := os.UserHomeDir()
			if herr != nil {
				return nil, "", fmt.Errorf("no kubeconfig: %w", err)
			}
			kc = filepath.Join(home, ".kube", "config")
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kc)
		if err != nil {
			return nil, "", err
		}
	}
	d, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, "", err
	}
	ns := os.Getenv("CAPI_NAMESPACE")
	if ns == "" {
		ns = "default"
	}
	return d, ns, nil
}

// gvr resolves the GVR for a CAPI resource across API versions.
func gvr(group, resource, version string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
}

// ProvisionNodePool creates a MachineDeployment plus its KubeadmConfigTemplate
// and provider infra template on the management cluster. The returned pool is
// registered in fleet state as Provisioning; nodes appear as the CAPI
// controller brings machines up.
func (c *CAPI) ProvisionNodePool(ctx context.Context, spec api.NodePoolCreateSpec) (*ProvisionResult, error) {
	if !c.Ready() {
		return nil, ErrNotConfigured
	}
	cfg := spec.Provider.Config
	clusterName := cfg["clusterName"]
	if clusterName == "" {
		return nil, fmt.Errorf("provider config %q is required", "clusterName")
	}
	ns := cfg["namespace"]
	if ns == "" {
		ns = c.ns
	}
	count := spec.DesiredNodeCount
	if count < 1 {
		count = 1
	}
	k8s := spec.KubernetesVersion
	if k8s == "" {
		k8s = "1.33.4"
	}

	kubeadm := kubeadmConfigTemplate(spec, ns, k8s)
	if err := c.applyUnstructured(ctx, "bootstrap.cluster.x-k8s.io", "kubeadmconfigtemplates", kubeadm); err != nil {
		return nil, fmt.Errorf("apply kubeadmconfigtemplate: %w", err)
	}
	infra, err := c.infraTemplate(spec, ns)
	if err != nil {
		return nil, err
	}
	if err := c.applyUnstructured(ctx, "infrastructure.cluster.x-k8s.io", infraResourceName(c.key, cfg), infra); err != nil {
		return nil, fmt.Errorf("apply infra template: %w", err)
	}
	md := machineDeployment(spec, ns, clusterName, count, k8s, c.templateKind(cfg))
	if err := c.applyUnstructured(ctx, "cluster.x-k8s.io", "machinedeployments", md); err != nil {
		return nil, fmt.Errorf("apply machinedeployment: %w", err)
	}

	now := time.Now()
	pool := api.NodePool{
		ID:                "pool-" + spec.Name,
		Name:              spec.Name,
		NodeNamePrefix:    spec.NodeNamePrefix,
		KubernetesVersion: k8s,
		DesiredNodeCount:  count,
		Provider:          spec.Provider,
		Conditions: []api.Condition{{
			Type: "Provisioning", Status: "True", Reason: "MachineDeploymentCreated",
			Message: "CAPI is reconciling the MachineDeployment", LastTransitionTime: now,
		}},
		NodeSets: []api.NodeSet{{
			ID:                "pool-" + spec.Name + "-set-0",
			NodePoolID:        "pool-" + spec.Name,
			DesiredCount:      count,
			KubernetesVersion: k8s,
			Provider:          spec.Provider,
		}},
	}
	return &ProvisionResult{Pool: pool}, nil
}

// DrainNode deletes the Machine backing the node — the CAPI machine
// controller cordons and drains the workload node, then the owning
// MachineDeployment creates a replacement.
func (c *CAPI) DrainNode(ctx context.Context, nodeID string) error {
	if !c.Ready() {
		return ErrNotConfigured
	}
	var lastErr error
	for _, v := range capiVersions {
		err := c.dyn.Resource(gvr("cluster.x-k8s.io", "machines", v)).Namespace(c.ns).
			Delete(ctx, nodeID, metav1.DeleteOptions{})
		switch {
		case err == nil:
			return nil
		case errors.IsNotFound(err):
			// Try name lookup against the live list before next version.
			found, lerr := c.deleteMachineByName(ctx, v, nodeID)
			if lerr == nil && found {
				return nil
			}
			if lerr != nil {
				lastErr = lerr
			}
		default:
			lastErr = err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("machine %q not found in namespace %q", nodeID, c.ns)
}

// deleteMachineByName deletes the first Machine whose metadata.name matches
// nodeID in this API version (IDs in fleet state may differ from object names).
func (c *CAPI) deleteMachineByName(ctx context.Context, version, nodeID string) (bool, error) {
	list, err := c.dyn.Resource(gvr("cluster.x-k8s.io", "machines", version)).Namespace(c.ns).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, m := range list.Items {
		if m.GetName() == nodeID {
			if err := c.dyn.Resource(gvr("cluster.x-k8s.io", "machines", version)).Namespace(c.ns).
				Delete(ctx, m.GetName(), metav1.DeleteOptions{}); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

// applyUnstructured upserts obj against the resource across CAPI API
// versions: update where the object already exists, otherwise create under
// the first version the API server accepts.
func (c *CAPI) applyUnstructured(ctx context.Context, group, resource string, obj *unstructuredObj) error {
	name, ns := obj.obj.GetName(), obj.obj.GetNamespace()
	var lastErr error
	for _, v := range capiVersions {
		ri := c.dyn.Resource(gvr(group, resource, v)).Namespace(ns)
		existing, err := ri.Get(ctx, name, metav1.GetOptions{})
		switch {
		case err == nil:
			u := obj.obj.DeepCopy()
			u.SetAPIVersion(group + "/" + v)
			u.SetResourceVersion(existing.GetResourceVersion())
			if _, err = ri.Update(ctx, u, metav1.UpdateOptions{}); err == nil {
				return nil
			}
		case errors.IsNotFound(err):
			u := obj.obj.DeepCopy()
			u.SetAPIVersion(group + "/" + v)
			if _, err = ri.Create(ctx, u, metav1.CreateOptions{}); err == nil {
				return nil
			}
		}
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}
