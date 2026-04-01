package resources

import (
	"context"
	"slices"

	"github.com/k8s-manifest-kit/engine/pkg/pipeline"
	"github.com/k8s-manifest-kit/engine/pkg/postrenderer"
	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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

// SortByApplyOrderWithCertificates extends the standard apply ordering to include
// cert-manager resources with proper dependency ordering: ClusterIssuer/Issuer before
// Certificate before workload resources that consume Certificate-generated Secrets.
func SortByApplyOrderWithCertificates(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	// First, apply standard k8s ordering
	ordered, err := SortByApplyOrder(ctx, resources)
	if err != nil {
		return nil, err
	}

	// Second, fine-tune cert-manager resource ordering within their tier
	return applyCertificateOrdering(ordered), nil
}

// applyCertificateOrdering ensures cert-manager resources are positioned correctly
// within the upstream-ordered list. This preserves the sophisticated upstream ordering
// while repositioning only cert-manager resources to resolve dependency issues.
func applyCertificateOrdering(resources []unstructured.Unstructured) []unstructured.Unstructured {
	if len(resources) == 0 {
		return resources
	}

	// Separate cert-manager resources from others and order them internally
	otherResources, certManagerIssuers, certManagerCertificates := categorizeCertManagerResources(resources)

	// Find where to insert cert-manager resources in the upstream-ordered list
	insertionPoint := findInsertionPointAfterServices(otherResources)

	// Assemble final result: upstream order + cert-manager before workloads
	return slices.Concat(
		otherResources[:insertionPoint], // Foundation resources (Namespace, CRD, Service, etc.)
		certManagerIssuers,              // ClusterIssuer, Issuer
		certManagerCertificates,         // Certificate
		otherResources[insertionPoint:], // Workloads and Webhooks
	)
}

// findInsertionPointAfterServices finds where to insert cert-manager resources
// after Services in the upstream-ordered list. This eliminates hardcoding of
// workload resource types while ensuring certificates come before workloads.
func findInsertionPointAfterServices(resources []unstructured.Unstructured) int {
	lastServiceIndex := -1

	// Find the position after the last Service resource
	for i, resource := range resources {
		resourceGVK := resource.GroupVersionKind()
		if resourceGVK == gvk.Service {
			lastServiceIndex = i
		}
	}

	// Insert after the last Service, or at the beginning if no Services exist
	if lastServiceIndex >= 0 {
		return lastServiceIndex + 1
	}

	// If no Services, find the first Deployment or StatefulSet
	// These are the primary workloads that actually consume certificate secrets
	for i, resource := range resources {
		resourceGVK := resource.GroupVersionKind()
		if resourceGVK == gvk.Deployment || resourceGVK == gvk.StatefulSet {
			return i // Insert before first workload that uses certificates
		}
	}

	// If only foundation resources exist, append at the end
	return len(resources)
}

// categorizeCertManagerResources separates cert-manager resources into dependency groups
// and returns them ordered for proper application: issuers before certificates.
func categorizeCertManagerResources(resources []unstructured.Unstructured) (
	[]unstructured.Unstructured,
	[]unstructured.Unstructured,
	[]unstructured.Unstructured,
) {
	var otherResources []unstructured.Unstructured
	var certManagerIssuers []unstructured.Unstructured
	var certManagerCertificates []unstructured.Unstructured

	for _, resource := range resources {
		if !isCertManagerResource(resource) {
			otherResources = append(otherResources, resource)
			continue
		}

		// Group cert-manager resources by dependency order
		switch resource.GetKind() {
		case "ClusterIssuer", "Issuer":
			certManagerIssuers = append(certManagerIssuers, resource)
		case "Certificate":
			certManagerCertificates = append(certManagerCertificates, resource)
		default:
			// Unknown cert-manager resources go with certificates (safer default)
			certManagerCertificates = append(certManagerCertificates, resource)
		}
	}

	return otherResources, certManagerIssuers, certManagerCertificates
}

// isCertManagerResource checks if a resource belongs to the cert-manager group.
// This approach is more extensible than hardcoding specific GVKs.
func isCertManagerResource(resource unstructured.Unstructured) bool {
	return resource.GroupVersionKind().Group == "cert-manager.io"
}
