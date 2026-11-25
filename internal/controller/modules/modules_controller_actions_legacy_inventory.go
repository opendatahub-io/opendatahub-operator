package modules

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// legacyModuleResourceKinds is pinned to the removable operator resource kinds
// shipped in ODH and RHOAI 3.6, including early-access manifests. Current
// manifest compliance has its own allowlist.
var legacyModuleResourceKinds = []schema.GroupVersionKind{
	gvk.ConfigMap,
	gvk.Service,
	gvk.ServiceAccount,
	gvk.Deployment,
	gvk.Role,
	gvk.RoleBinding,
	gvk.ClusterRole,
	gvk.ClusterRoleBinding,
	gvk.MutatingWebhookConfiguration,
	gvk.ValidatingAdmissionPolicy,
	gvk.ValidatingAdmissionPolicyBinding,
	gvk.CertManagerCertificate,
	gvk.CertManagerIssuer,
	gvk.CoreosPrometheusRule,
	gvk.CoreosServiceMonitor,
	gvk.ServiceMonitor,
	gvk.NetworkPolicy,
	gvk.SecurityContextConstraints,
}

func listLegacyModuleResources(ctx context.Context, rr *odhtype.ReconciliationRequest) ([]unstructured.Unstructured, error) {
	platform, ok := rr.Instance.(*configApi.Platform)
	if !ok {
		return nil, fmt.Errorf("legacy resource discovery requires Platform instance, got %T", rr.Instance)
	}

	var candidates []unstructured.Unstructured
	for _, kind := range legacyModuleResourceKinds {
		list := &unstructured.UnstructuredList{}
		// The cache uses the item GVK to select the informer.
		list.SetGroupVersionKind(kind)
		if err := rr.Client.List(ctx, list); err != nil {
			if meta.IsNoMatchError(err) {
				continue
			}
			return nil, fmt.Errorf("listing legacy %s: %w", kind, err)
		}
		for i := range list.Items {
			obj := &list.Items[i]
			obj.SetGroupVersionKind(kind)
			if isLegacyModuleResource(platform, rr.Release, obj) {
				candidates = append(candidates, *obj)
			}
		}
	}
	return candidates, nil
}
