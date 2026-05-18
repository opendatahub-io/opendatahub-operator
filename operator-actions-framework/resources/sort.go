package resources

import (
	"context"

	"github.com/k8s-manifest-kit/engine/pkg/pipeline"
	"github.com/k8s-manifest-kit/engine/pkg/postrenderer"
	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Well-known GVKs used for resource ordering.
var (
	gvkCertManagerClusterIssuer         = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuer"}
	gvkCertManagerIssuer                = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Issuer"}
	gvkCertManagerCertificate           = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"}
	gvkDeployment                       = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	gvkStatefulSet                      = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	gvkDaemonSet                        = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}
	gvkJob                              = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}
	gvkCronJob                          = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "CronJob"}
	gvkMutatingWebhook                  = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "MutatingWebhookConfiguration"}
	gvkValidatingWebhook                = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingWebhookConfiguration"}
	gvkValidatingAdmissionPolicy        = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicy"}
	gvkValidatingAdmissionPolicyBinding = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicyBinding"}
)

var defaultPostRenderers = []engineTypes.PostRenderer{
	postrenderer.ApplyOrder(),
	CertManagerPostRenderer(),
}

// SortByApplyOrder reorders resources into dependency order for cluster
// application: foundational resources (Namespace, CRD, etc.) first,
// cert-manager resources before workloads, and webhooks last.
func SortByApplyOrder(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	return pipeline.ApplyPostRenderers(ctx, resources, defaultPostRenderers)
}

// CertManagerPostRenderer returns a PostRenderer that orders cert-manager resources
// in dependency order: ClusterIssuer → Issuer → Certificate before Deployments.
func CertManagerPostRenderer() engineTypes.PostRenderer {
	return func(ctx context.Context, objects []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
		if len(objects) == 0 {
			return objects, nil
		}

		var certManagerResources []unstructured.Unstructured
		var otherResources []unstructured.Unstructured

		for _, resource := range objects {
			if isCertManagerResource(resource) {
				certManagerResources = append(certManagerResources, resource)
			} else {
				otherResources = append(otherResources, resource)
			}
		}

		if len(certManagerResources) == 0 {
			return objects, nil
		}

		insertIndex := len(otherResources)
		for i, resource := range otherResources {
			if isWorkloadKind(resource) || IsWebhookResource(resource) {
				insertIndex = i
				break
			}
		}

		result := make([]unstructured.Unstructured, 0, len(objects))
		result = append(result, otherResources[:insertIndex]...)

		for _, targetGVK := range []schema.GroupVersionKind{gvkCertManagerClusterIssuer, gvkCertManagerIssuer, gvkCertManagerCertificate} {
			for _, resource := range certManagerResources {
				if resource.GroupVersionKind() == targetGVK {
					result = append(result, resource)
				}
			}
		}

		result = append(result, otherResources[insertIndex:]...)
		return result, nil
	}
}

func isCertManagerResource(r unstructured.Unstructured) bool {
	g := r.GroupVersionKind()
	return g == gvkCertManagerClusterIssuer ||
		g == gvkCertManagerIssuer ||
		g == gvkCertManagerCertificate
}

func isWorkloadKind(r unstructured.Unstructured) bool {
	g := r.GroupVersionKind()
	return g == gvkDeployment ||
		g == gvkStatefulSet ||
		g == gvkDaemonSet ||
		g == gvkJob ||
		g == gvkCronJob
}

// IsWebhookResource returns true if the resource is a webhook configuration.
func IsWebhookResource(r unstructured.Unstructured) bool {
	g := r.GroupVersionKind()
	return g == gvkMutatingWebhook ||
		g == gvkValidatingWebhook ||
		g == gvkValidatingAdmissionPolicy ||
		g == gvkValidatingAdmissionPolicyBinding
}
