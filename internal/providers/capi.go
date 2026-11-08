package providers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nutcas3/ai-engine/internal/api"
	"github.com/nutcas3/ai-engine/internal/gitops"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	ready   bool
}

// NewCAPI builds a provider for key bound to the management-cluster client.
// ready reports whether the core CAPI CRDs AND this provider's own infra
// CRDs were verified — a resolvable kubeconfig alone is not enough.
func NewCAPI(key, display string, dyn dynamic.Interface, ns string, ready bool) *CAPI {
	return &CAPI{key: key, display: display, dyn: dyn, ns: ns, ready: ready}
}

// requiredEnv maps each provider to alternative credential sets: the adapter
// is configured when ANY complete set is present in the environment. These
// mirror the env contract each CAPI controller expects (clusterctl vars /
// cloud credentials). Bare-metal providers gate on inventory instead — no
// cloud creds.
var requiredEnv = map[string][][]string{
	"aws":       {{"AWS_B64ENCODED_CREDENTIALS"}, {"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}},
	"gcp":       {{"GCP_B64ENCODED_CREDENTIALS"}},
	"azure":     {{"AZURE_SUBSCRIPTION_ID", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET", "AZURE_TENANT_ID"}},
	"vsphere":   {{"VSPHERE_SERVER", "VSPHERE_USERNAME", "VSPHERE_PASSWORD"}},
	"openstack": {{"OS_CLOUD"}, {"OS_APPLICATION_CREDENTIAL_ID"}},
	"proxmox":   {{"PROXMOX_URL", "PROXMOX_TOKEN"}},
	"tinkerbell": {{"TINKERBELL_IP"}},
	"coreweave": {{"COREWEAVE_API_TOKEN"}},
	// metal3, nico: inventory/BlueField-based, no cloud credential env
}

func (c *CAPI) Key() string     { return c.key }
func (c *CAPI) Display() string { return c.display }

// Ready requires kubeconfig+CRDs (probed at boot) AND provider credentials
// present in the environment — checked live so exported vars take effect
// without a restart.
func (c *CAPI) Ready() bool {
	return c.dyn != nil && c.ready && len(c.MissingConfig()) == 0
}

// MissingConfig reports the unmet credential set for this provider, picking
// the alternative closest to satisfied so the hint is actionable.
func (c *CAPI) MissingConfig() []string {
	if c.dyn == nil {
		return []string{"CAPI_KUBECONFIG"}
	}
	if !c.ready {
		return []string{"CAPI CRDs on management cluster"}
	}
	sets, ok := requiredEnv[c.key]
	if !ok || len(sets) == 0 {
		return nil
	}
	var best []string
	for _, set := range sets {
		var missing []string
		for _, v := range set {
			if os.Getenv(v) == "" {
				missing = append(missing, v)
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if best == nil || len(missing) < len(best) {
			best = missing
		}
	}
	return best
}

// probeResource checks whether one GVR is served, trying both API versions
// with a short timeout. Used for the CAPI core check and each provider's own
// infra CRDs.
func probeResource(ctx context.Context, dyn dynamic.Interface, group, resource string) bool {
	for _, v := range capiVersions {
		tctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		_, err := dyn.Resource(gvr(group, resource, v)).
			Namespace("").List(tctx, metav1.ListOptions{Limit: 1})
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}

// probeProvider reports whether this adapter can actually provision: the CAPI
// core CRDs AND the provider's own infra cluster CRD must be served (CAPA
// installs awsclusters, CAPG gcpclusters, CAPZ azureclusters, CAPM3
// metal3clusters, NICo nicoclusters — overridable via clusterResource).
func (c *CAPI) probeProvider(ctx context.Context) bool {
	if c.dyn == nil {
		return false
	}
	if !probeResource(ctx, c.dyn, "cluster.x-k8s.io", "clusters") {
		return false
	}
	return probeResource(ctx, c.dyn, "infrastructure.cluster.x-k8s.io",
		infraClusterResourceName(c.key, nil))
}

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
	count := max(spec.DesiredNodeCount, 1)
	k8s := spec.KubernetesVersion
	if k8s == "" {
		k8s = "1.33.4"
	}

	kubeadm := kubeadmConfigTemplate(spec, ns)
	bootVer, err := c.applyUnstructured(ctx, "bootstrap.cluster.x-k8s.io", "kubeadmconfigtemplates", kubeadm)
	if err != nil {
		return nil, fmt.Errorf("apply kubeadmconfigtemplate: %w", err)
	}
	infra, err := c.infraTemplate(spec, ns)
	if err != nil {
		return nil, err
	}
	infraVer, err := c.applyUnstructured(ctx, "infrastructure.cluster.x-k8s.io", infraResourceName(c.key, cfg), infra)
	if err != nil {
		return nil, fmt.Errorf("apply infra template: %w", err)
	}
	md := machineDeployment(spec, ns, clusterName, count, k8s, c.templateKind(cfg), bootVer, infraVer)
	if _, err := c.applyUnstructured(ctx, "cluster.x-k8s.io", "machinedeployments", md); err != nil {
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

// ProvisionCluster applies the day-0 CAPI objects for a whole cluster:
// <Kind>Cluster, a control-plane <Kind>MachineTemplate, KubeadmControlPlane,
// and the Cluster object wiring them. Optional initial node pools are
// applied afterwards via ProvisionNodePool with clusterName injected.
func (c *CAPI) ProvisionCluster(ctx context.Context, req api.CreateClusterRequest) (*api.Cluster, error) {
	if !c.Ready() {
		return nil, ErrNotConfigured
	}
	cfg := req.Provider.Config
	ns := cfg["namespace"]
	if ns == "" {
		ns = c.ns
	}
	k8s := req.ControlPlaneKubernetesVersion
	if k8s == "" {
		k8s = "1.33.4"
	}
	tier := req.Tier
	if tier == "" {
		tier = "optimal"
	}
	cpCount := int64(1)
	if v := cfg["controlPlaneCount"]; v != "" {
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			cpCount = n
		}
	}

	// 1. Infrastructure cluster (AWSCluster, GCPCluster, Metal3Cluster, …).
	ic := c.infraCluster(req.Name, ns, cfg)
	infraVer, err := c.applyUnstructured(ctx, "infrastructure.cluster.x-k8s.io", infraClusterResourceName(c.key, cfg), ic)
	if err != nil {
		return nil, fmt.Errorf("apply infra cluster: %w", err)
	}
	// 2. Control-plane machine template.
	spec := api.NodePoolCreateSpec{
		Name: req.Name + "-cp", NodeNamePrefix: "cp",
		DesiredNodeCount: int(cpCount), KubernetesVersion: k8s,
		Provider: req.Provider,
	}
	cpMT, err := c.infraTemplate(spec, ns)
	if err != nil {
		return nil, err
	}
	cpMTVer, err := c.applyUnstructured(ctx, "infrastructure.cluster.x-k8s.io", infraResourceName(c.key, cfg), cpMT)
	if err != nil {
		return nil, fmt.Errorf("apply cp machine template: %w", err)
	}
	// 3. KubeadmControlPlane.
	kcp := kubeadmControlPlane(req.Name, ns, k8s, cpCount, c.templateKind(cfg), cpMTVer)
	kcpVer, err := c.applyUnstructured(ctx, "controlplane.cluster.x-k8s.io", "kubeadmcontrolplanes", kcp)
	if err != nil {
		return nil, fmt.Errorf("apply kubeadmcontrolplane: %w", err)
	}
	// 4. Cluster object wiring both refs.
	cl := clusterObj(req.Name, ns, tier, c.infraClusterKind(cfg), kcpVer, infraVer)
	if _, err := c.applyUnstructured(ctx, "cluster.x-k8s.io", "clusters", cl); err != nil {
		return nil, fmt.Errorf("apply cluster: %w", err)
	}
	// 5. Day-1 GPU layer: HelmChartProxy installs GPU Operator + DRA driver +
	// DCGM exporter on every matching workload cluster automatically.
	proxy := gpuOperatorProxy(ns)
	if _, err := c.applyUnstructured(ctx, "addons.cluster.x-k8s.io", "helmchartproxies", proxy); err != nil {
		return nil, fmt.Errorf("apply gpu-operator helmchartproxy: %w", err)
	}
	// 6. Day-0 GitOps handoff: the tier's root Application in argocd syncs
	// the ai-for-k8s layer manifests onto the workload cluster, and a Kueue
	// ClusterQueue seeds the tenant's GPU quota.
	app := gitops.RenderRootApp(req.Name, tier, cfg["gitopsRepo"])
	if _, err := c.applyUnstructured(ctx, "argoproj.io", "applications", &unstructuredObj{obj: app}); err != nil {
		return nil, fmt.Errorf("apply argocd root app: %w", err)
	}
	queue := gitops.RenderClusterQueue(req.Name, orDefault(cfg["tenant"], "default"), 4)
	if _, err := c.applyUnstructured(ctx, "kueue.x-k8s.io", "clusterqueues", &unstructuredObj{obj: queue}); err != nil {
		return nil, fmt.Errorf("apply kueue clusterqueue: %w", err)
	}

	// 5. Optional initial worker pools — same flow as POST /api/nodePools.
	poolNames := make([]string, 0, len(req.NodePools))
	for _, p := range req.NodePools {
		p.Provider = req.Provider
		if p.Provider.Config == nil {
			p.Provider.Config = map[string]string{}
		}
		p.Provider.Config["clusterName"] = req.Name
		if _, err := c.ProvisionNodePool(ctx, p); err != nil {
			return nil, fmt.Errorf("provision pool %q: %w", p.Name, err)
		}
		poolNames = append(poolNames, p.Name)
	}

	now := time.Now()
	return &api.Cluster{
		ID: "cluster-" + req.Name, Name: req.Name, Tier: tier,
		ControlPlaneKubernetesVersion: k8s, CNIPlugin: cfg["cniPlugin"],
		EtcdEncrypted: req.EtcdEncrypted,
		Network: api.ClusterNetwork{
			PodCIDR:     orDefault(cfg["podCIDR"], "192.168.0.0/16"),
			ServiceCIDR: orDefault(cfg["serviceCIDR"], "10.96.0.0/12"),
		},
		Provider:  req.Provider,
		CreatedAt: now,
		Conditions: []api.Condition{{
			Type: "Provisioning", Status: "True", Reason: "ClusterObjectsApplied",
			Message: "CAPI is reconciling the cluster (pools: " + fmt.Sprintf("%v", poolNames) + ")",
			LastTransitionTime: now,
		}},
	}, nil
}

func orDefault(v, d string) string {
	if v != "" {
		return v
	}
	return d
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
		nodeRefName, _, _ := unstructured.NestedString(m.Object, "status", "nodeRef", "name")
		if m.GetName() == nodeID || nodeRefName == nodeID || m.GetLabels()["node"] == nodeID {
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
// the first version the API server accepts. It returns the version applied
// so callers can write the same apiVersion into cross-object refs.
func (c *CAPI) applyUnstructured(ctx context.Context, group, resource string, obj *unstructuredObj) (string, error) {
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
				return v, nil
			}
		case errors.IsNotFound(err):
			u := obj.obj.DeepCopy()
			u.SetAPIVersion(group + "/" + v)
			if _, err = ri.Create(ctx, u, metav1.CreateOptions{}); err == nil {
				return v, nil
			}
		}
		if err != nil {
			lastErr = err
		}
	}
	return "", lastErr
}

// ProvisionEndpoint applies the serving objects for an inference endpoint to
// the serving namespace on the management cluster: KServe LLMInferenceService
// + GIE InferencePool + the tenant's Kueue LocalQueue.
func (c *CAPI) ProvisionEndpoint(ctx context.Context, e api.InferenceEndpoint, tenant string) error {
	if !c.Ready() {
		return ErrNotConfigured
	}
	ns := "serving"
	if tenant == "" {
		tenant = "default"
	}
	objs := []struct{ group, res string; obj *unstructuredObj }{
		{"serving.kserve.io", "llminferenceservices", &unstructuredObj{obj: gitops.RenderLLMInferenceService(struct {
			Name, Model, PoolID string; Replicas int
		}{e.Name, e.Model, e.PoolID, e.Replicas})}},
		{"inference.networking.x-k8s.io", "inferencepools", &unstructuredObj{obj: gitops.RenderInferencePool(e.Name+"-pool", e.PoolID, 8000)}},
		{"kueue.x-k8s.io", "localqueues", &unstructuredObj{obj: gitops.RenderLocalQueue(orDefault(e.PoolID, "default"), tenant, ns)}},
	}
	for _, o := range objs {
		o.obj.obj.SetNamespace(ns)
		if _, err := c.applyUnstructured(ctx, o.group, o.res, o.obj); err != nil {
			return fmt.Errorf("apply %s/%s: %w", o.group, o.res, err)
		}
	}
	return nil
}
