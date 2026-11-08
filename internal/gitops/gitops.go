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
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]interface{}{
			"name":      fmt.Sprintf("%s-%s-root", cluster, tier),
			"namespace": "argocd",
			"labels": map[string]interface{}{
				"ai-engine.io/cluster": cluster,
				"ai-engine.io/tier":    tier,
			},
			"finalizers": []interface{}{"resources-finalizer.argocd.argoproj.io"},
		},
		"spec": map[string]interface{}{
			"project": "default",
			"source": map[string]interface{}{
				"repoURL":        repoURL,
				"targetRevision": "HEAD",
				"path":           fmt.Sprintf("deploy/clusters/%s", tier),
			},
			"destination": map[string]interface{}{
				"server":    "https://kubernetes.default.svc",
				"namespace": "argocd",
			},
			"syncPolicy": map[string]interface{}{
				"automated":   map[string]interface{}{"prune": true, "selfHeal": true},
				"syncOptions": []interface{}{"CreateNamespace=true", "ServerSideApply=true"},
			},
		},
	}}
}

// RenderClusterQueue builds the Kueue ClusterQueue granting a tenant its GPU
// quota — the tenancy mechanism for workloads on the new cluster.
func RenderClusterQueue(cluster, tenant string, gpus int64) *unstructured.Unstructured {
	name := fmt.Sprintf("%s-%s-queue", cluster, tenant)
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "ClusterQueue",
		"metadata":   map[string]interface{}{"name": name},
		"spec": map[string]interface{}{
			"resourceGroups": []interface{}{
				map[string]interface{}{
					"coveredResources": []interface{}{"nvidia.com/gpu"},
					"flavors": []interface{}{
						map[string]interface{}{
							"name": "default-flavor",
							"resources": []interface{}{
								map[string]interface{}{"name": "nvidia.com/gpu", "nominalQuota": gpus},
							},
						},
					},
				},
			},
		},
	}}
}
