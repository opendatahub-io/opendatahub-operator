package resources

import (
	"context"

	"github.com/k8s-manifest-kit/engine/pkg/pipeline"
	"github.com/k8s-manifest-kit/engine/pkg/postrenderer"
	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var defaultPostRenderers = []engineTypes.PostRenderer{
	postrenderer.ApplyOrder(),
}

// SortByApplyOrder reorders resources into dependency order for cluster
// application: foundational resources (Namespace, CRD, etc.) first,
// webhooks last.
func SortByApplyOrder(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	return pipeline.ApplyPostRenderers(ctx, resources, defaultPostRenderers)
}

// SortByApplyOrderWithCertificates applies upstream k8s-manifest-kit/engine ordering first,
// then enhances it by inserting cert-manager resources in dependency order after Secrets.
func SortByApplyOrderWithCertificates(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	// First, apply upstream ordering to get standard Kubernetes resource ordering
	sorted, err := SortByApplyOrder(ctx, resources)
	if err != nil {
		return nil, err
	}

	// Extract cert-manager resources from the sorted list
	var certManagerResources []unstructured.Unstructured
	var otherResources []unstructured.Unstructured

	for _, resource := range sorted {
		if isCertManagerResource(resource.GetKind()) {
			certManagerResources = append(certManagerResources, resource)
		} else {
			otherResources = append(otherResources, resource)
		}
	}

	// If no cert-manager resources, return upstream result as-is
	if len(certManagerResources) == 0 {
		return sorted, nil
	}

	// Find insertion point: before the first Deployment
	insertIndex := len(otherResources) // Default: insert at end
	for i, resource := range otherResources {
		if resource.GetKind() == "Deployment" {
			insertIndex = i
			break
		}
	}

	// Insert cert-manager resources in dependency order: ClusterIssuer, Issuer, Certificate
	result := make([]unstructured.Unstructured, 0, len(sorted))
	result = append(result, otherResources[:insertIndex]...)

	// Add cert-manager resources in dependency order
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

// isCertManagerResource checks if the given kind is a cert-manager resource type.
func isCertManagerResource(kind string) bool {
	return kind == "ClusterIssuer" || kind == "Issuer" || kind == "Certificate"
}
