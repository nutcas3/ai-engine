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
	{"bootstrap.cluster.x-k8s.io", "kubeadmconfigtemplates"},
	{"infrastructure.cluster.x-k8s.io", "awsmachinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "gcpmachinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "azuremachinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "metal3machinetemplates"},
	{"infrastructure.cluster.x-k8s.io", "nicomachinetemplates"},
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
	p := NewCAPI("aws", "test", fakeDyn(), "default")
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

func TestCAPIDrainDeletesMachine(t *testing.T) {
	machine := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "cluster.x-k8s.io/v1beta1",
		"kind":       "Machine",
		"metadata":   map[string]interface{}{"name": "gpu-node-1", "namespace": "default"},
	}}
	p := NewCAPI("aws", "test", fakeDyn(machine), "default")
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
	p := NewCAPI("aws", "test", nil, "default")
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
