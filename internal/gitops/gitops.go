// Package gitops renders the day-0 handoff objects: the ArgoCD root
// Application that syncs a cluster's tier profile from the ai-for-k8s
// content repo, plus the Kueue tenancy objects created per cluster.
package gitops

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const DefaultRepoURL = "https://github.com/nutcas3/ai-for-k8s"

// RenderRootApp builds the ArgoCD Application pointing at deploy/clusters/
// <tier> in the GitOps content repo — applied to the management cluster's
// argocd namespace at cluster create so day-2 layers sync themselves.
func RenderRootApp(cluster, tier, repoURL string) *unstructured.Unstructured {
	if repoURL == "" {
		repoURL = DefaultRepoURL
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("%s-%s-root", cluster, tier),
			"namespace": "argocd",
			"labels": map[string]any{
				"ai-engine.io/cluster": cluster,
				"ai-engine.io/tier":    tier,
			},
			"finalizers": []any{"resources-finalizer.argocd.argoproj.io"},
		},
		"spec": map[string]any{
			"project": "default",
			"source": map[string]any{
				"repoURL":        repoURL,
				"targetRevision": "HEAD",
				"path":           fmt.Sprintf("deploy/clusters/%s", tier),
			},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": "argocd",
			},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"prune": true, "selfHeal": true},
				"syncOptions": []any{"CreateNamespace=true", "ServerSideApply=true"},
			},
		},
	}}
}

// RenderClusterQueue builds the Kueue ClusterQueue granting a tenant its GPU
// quota — the tenancy mechanism for workloads on the new cluster.
func RenderClusterQueue(cluster, tenant string, gpus int64) *unstructured.Unstructured {
	name := fmt.Sprintf("%s-%s-queue", cluster, tenant)
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "ClusterQueue",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"resourceGroups": []any{
				map[string]any{
					"coveredResources": []any{"nvidia.com/gpu"},
					"flavors": []any{
						map[string]any{
							"name": "default-flavor",
							"resources": []any{
								map[string]any{"name": "nvidia.com/gpu", "nominalQuota": gpus},
							},
						},
					},
				},
			},
		},
	}}
}

// RenderLLMInferenceService builds a KServe LLMInferenceService for an
// endpoint — the real object applied to the workload/management cluster.
func RenderLLMInferenceService(e struct {
	Name, Model, PoolID string
	Replicas            int
}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "serving.kserve.io/v1beta1",
		"kind":       "LLMInferenceService",
		"metadata":   map[string]any{"name": e.Name, "namespace": "serving"},
		"spec": map[string]any{
			"replicas": int64(e.Replicas),
			"model":    map[string]any{"name": e.Model},
			"pool":     map[string]any{"nodePool": e.PoolID},
		},
	}}
}

// RenderLocalQueue builds the Kueue LocalQueue binding a namespace to a
// ClusterQueue — the per-team tenancy object.
func RenderLocalQueue(cluster, tenant, ns string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "LocalQueue",
		"metadata":   map[string]any{"name": tenant + "-queue", "namespace": ns},
		"spec": map[string]any{
			"clusterQueue": cluster + "-" + tenant + "-queue",
		},
	}}
}

// RenderInferencePool builds a GIE InferencePool for the serving extension.
func RenderInferencePool(name, poolID string, ports int32) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "inference.networking.x-k8s.io/v1alpha1",
		"kind":       "InferencePool",
		"metadata":   map[string]any{"name": name, "namespace": "serving"},
		"spec": map[string]any{
			"selector":         map[string]any{"nodePool": poolID},
			"targetPortNumber": ports,
		},
	}}
}
