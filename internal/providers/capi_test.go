package providers

import (
	"context"
	"testing"

	"ai-engine/internal/api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var capiResources = []struct{ group, res string }{
	{"cluster.x-k8s.io", "machines"},
	{"cluster.x-k8s.io", "machinedeployments"},
	{"cluster.x-k8s.io", "clusters"},
	{"bootstrap.cluster.x-k8s.io", "kubeadmconfigtemplates"},
	{"controlplane.cluster.x-k8s.io", "kubeadmcontrolplanes"},
	{"addons.cluster.x-k8s.io", "helmchartproxies"},
	{"infrastructure.cluster.x-k8s.io", "awsmachinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "gcpmachinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "azuremachinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "metal3machinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "nicomachinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "awsclusters"},
	{"infrastructure.cluster.x-k8s.io", "gcpclusters"},
	{"infrastructure.cluster.x-k8s.io", "azureclusters"},
	{"infrastructure.cluster.x-k8s.io", "metal3clusters"},
	{"infrastructure.cluster.x-k8s.io", "nicoclusters"},
	{"argoproj.io", "applications"},
	{"kueue.x-k8s.io", "clusterqueues"},
}

func fakeDyn(objs ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	lists := map[schema.GroupVersionResource]string{}
	for _, r := range capiResources {
		for _, v := range capiVersions {
			lists[schema.GroupVersionResource{Group: r.group, Version: v, Resource: r.res}] = r.res + "List"
		}
	}
	items := make([]runtime.Object, 0, len(objs))
	for _, o := range objs {
		items = append(items, o)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, lists, items...)
}

func spec(name string) api.NodePoolCreateSpec {
	return api.NodePoolCreateSpec{
		Name:              name,
		DesiredNodeCount:  3,
		KubernetesVersion: "1.33.4",
		GPUModel:          "NVIDIA H100 80GB",
		Provider: api.InfraProvider{Kind: "aws", Config: map[string]string{
			"clusterName":  "gpu-fleet",
			"instanceType": "p4d.24xlarge",
			"region":       "us-east-1",
		}},
	}
}

func TestCAPIProvisionAppliesCAPIObjects(t *testing.T) {
	t.Setenv("AWS_B64ENCODED_CREDENTIALS", "x")
	p := NewCAPI("aws", "test", fakeDyn(), "default", true)
	res, err := p.ProvisionNodePool(context.Background(), spec("h100-pool"))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if res.Pool.DesiredNodeCount != 3 || res.Pool.Conditions[0].Type != "Provisioning" {
		t.Fatalf("unexpected pool: %+v", res.Pool)
	}

	ctx := context.Background()
	for _, check := range []struct{ group, res, name string }{
		{"bootstrap.cluster.x-k8s.io", "kubeadmconfigtemplates", "h100-pool-kubeadm"},
		{"infrastructure.cluster.x-k8s.io", "awsmachinetemplates", "h100-pool-mt"},
		{"cluster.x-k8s.io", "machinedeployments", "h100-pool"},
	} {
		var got *unstructured.Unstructured
		for _, v := range capiVersions {
			if o, err := p.dyn.Resource(schema.GroupVersionResource{Group: check.group, Version: v, Resource: check.res}).
				Namespace("default").Get(ctx, check.name, metav1.GetOptions{}); err == nil {
				got = o
				break
			}
		}
		if got == nil {
			t.Fatalf("%s/%s not applied", check.res, check.name)
		}
	}
}

func TestCAPIProvisionClusterAppliesDay0Objects(t *testing.T) {
	t.Setenv("AWS_B64ENCODED_CREDENTIALS", "x")
	p := NewCAPI("aws", "test", fakeDyn(), "default", true)
	cl, err := p.ProvisionCluster(context.Background(), api.CreateClusterRequest{
		Name:                          "gpu-fleet",
		Tier:                          "optimal",
		ControlPlaneKubernetesVersion: "1.33.4",
		Provider: api.InfraProvider{Kind: "aws", Config: map[string]string{
			"region": "us-east-1", "controlPlaneCount": "3",
		}},
		NodePools: []api.NodePoolCreateSpec{{
			Name: "h100-pool", DesiredNodeCount: 2, GPUModel: "NVIDIA H100 80GB",
		}},
	})
	if err != nil {
		t.Fatalf("provision cluster: %v", err)
	}
	if cl.Tier != "optimal" || cl.Conditions[0].Type != "Provisioning" {
		t.Fatalf("unexpected cluster: %+v", cl)
	}

	ctx := context.Background()
	for _, check := range []struct{ group, res, name string }{
		{"infrastructure.cluster.x-k8s.io", "awsclusters", "gpu-fleet"},
		{"infrastructure.cluster.x-k8s.io", "awsmachinetemplates", "gpu-fleet-cp-mt"},
		{"controlplane.cluster.x-k8s.io", "kubeadmcontrolplanes", "gpu-fleet-kcp"},
		{"cluster.x-k8s.io", "clusters", "gpu-fleet"},
		{"addons.cluster.x-k8s.io", "helmchartproxies", "gpu-operator"},
		{"cluster.x-k8s.io", "machinedeployments", "h100-pool"},
	} {
		var got *unstructured.Unstructured
		for _, v := range capiVersions {
			if o, err := p.dyn.Resource(schema.GroupVersionResource{Group: check.group, Version: v, Resource: check.res}).
				Namespace("default").Get(ctx, check.name, metav1.GetOptions{}); err == nil {
				got = o
				break
			}
		}
		if got == nil {
			t.Fatalf("%s/%s not applied", check.res, check.name)
		}
	}
	// Control-plane replica count flowed through.
	kcp, _ := p.dyn.Resource(schema.GroupVersionResource{Group: "controlplane.cluster.x-k8s.io", Version: capiVersions[0], Resource: "kubeadmcontrolplanes"}).
		Namespace("default").Get(ctx, "gpu-fleet-kcp", metav1.GetOptions{})
	if kcp == nil {
		kcp, _ = p.dyn.Resource(schema.GroupVersionResource{Group: "controlplane.cluster.x-k8s.io", Version: capiVersions[1], Resource: "kubeadmcontrolplanes"}).
			Namespace("default").Get(ctx, "gpu-fleet-kcp", metav1.GetOptions{})
	}
	if replicas, _, _ := unstructured.NestedInt64(kcp.Object, "spec", "replicas"); replicas != 3 {
		t.Fatalf("expected 3 control-plane replicas, got %d", replicas)
	}
}

func TestCAPIDrainDeletesMachine(t *testing.T) {
	t.Setenv("AWS_B64ENCODED_CREDENTIALS", "x")
	machine := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta1",
		"kind":       "Machine",
		"metadata":   map[string]any{"name": "gpu-node-1", "namespace": "default"},
	}}
	p := NewCAPI("aws", "test", fakeDyn(machine), "default", true)
	if err := p.DrainNode(context.Background(), "gpu-node-1"); err != nil {
		t.Fatalf("drain: %v", err)
	}
	for _, v := range capiVersions {
		if _, err := p.dyn.Resource(schema.GroupVersionResource{Group: "cluster.x-k8s.io", Version: v, Resource: "machines"}).
			Namespace("default").Get(context.Background(), "gpu-node-1", metav1.GetOptions{}); err == nil {
			t.Fatal("machine still exists after drain")
		}
	}
}

func TestCAPINotConfigured(t *testing.T) {
	p := NewCAPI("aws", "test", nil, "default", false)
	if p.Ready() {
		t.Fatal("expected Ready()=false with nil client")
	}
	if _, err := p.ProvisionNodePool(context.Background(), spec("x")); err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
	if err := p.DrainNode(context.Background(), "x"); err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}
