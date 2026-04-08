package resources

import (
	"context"

	"github.com/k8s-manifest-kit/engine/pkg/pipeline"
	"github.com/k8s-manifest-kit/engine/pkg/postrenderer"
	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var defaultPostRenderers = []engineTypes.PostRenderer{
	postrenderer.ApplyOrder(), // Standard K8s resource ordering
	CertManagerPostRenderer(), // Cert-manager dependency ordering
}

// SortByApplyOrder reorders resources into dependency order for cluster
// application: foundational resources (Namespace, CRD, etc.) first,
// cert-manager resources (ClusterIssuer/Issuer/Certificate) before workloads,
// and webhooks last.
func SortByApplyOrder(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	return pipeline.ApplyPostRenderers(ctx, resources, defaultPostRenderers)
}

// CertManagerPostRenderer returns a PostRenderer that orders cert-manager resources
// in dependency order: ClusterIssuer → Issuer → Certificate before Deployments.
// This prevents transient failures when Deployments consume Certificate-generated Secrets.
func CertManagerPostRenderer() engineTypes.PostRenderer {
	return func(ctx context.Context, objects []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
		// Early exit for empty input
		if len(objects) == 0 {
			return objects, nil
		}

		// Separate cert-manager resources from others
		var certManagerResources []unstructured.Unstructured
		var otherResources []unstructured.Unstructured

		for _, resource := range objects {
			if isCertManagerResource(resource.GetKind()) {
				certManagerResources = append(certManagerResources, resource)
			} else {
				otherResources = append(otherResources, resource)
			}
		}

		// If no cert-manager resources, return unchanged (zero overhead)
		if len(certManagerResources) == 0 {
			return objects, nil
		}

		// Find insertion point: before first Deployment
		insertIndex := len(otherResources) // Default: insert at end
		for i, resource := range otherResources {
			if resource.GetKind() == "Deployment" {
				insertIndex = i
				break
			}
		}

		// Build result with cert-manager resources in dependency order
		result := make([]unstructured.Unstructured, 0, len(objects))
		result = append(result, otherResources[:insertIndex]...)

		// Add cert-manager resources in dependency order: ClusterIssuer, Issuer, Certificate
		for _, kind := range []string{"ClusterIssuer", "Issuer", "Certificate"} {
			for _, resource := range certManagerResources {
				if resource.GetKind() == kind {
					result = append(result, resource)
				}
			}
		}

		result = append(result, otherResources[insertIndex:]...)
		return result, nil
	}
}

// isCertManagerResource checks if the given kind is a cert-manager resource type.
func isCertManagerResource(kind string) bool {
	return kind == "ClusterIssuer" || kind == "Issuer" || kind == "Certificate"
}
