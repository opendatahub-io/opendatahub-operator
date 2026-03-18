package envt

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// RegisterCertManagerCRDs registers the three core cert-manager CRDs (Certificate, Issuer,
// and ClusterIssuer) in the test environment. Returns the registered CRD objects, which
// callers can pass to CleanupDelete if individual cleanup is needed.
//
// Pass WithPermissiveSchema() when the test creates or writes resources of these types
// (e.g., via the deploy action). Tests that only check CRD presence do not need it.
func (et *EnvT) RegisterCertManagerCRDs(ctx context.Context, opts ...CRDOption) ([]*apiextensionsv1.CustomResourceDefinition, error) {
	defs := []struct {
		gvkDef   schema.GroupVersionKind
		plural   string
		singular string
		scope    apiextensionsv1.ResourceScope
	}{
		{gvk.CertManagerCertificate, "certificates", "certificate", apiextensionsv1.NamespaceScoped},
		{gvk.CertManagerIssuer, "issuers", "issuer", apiextensionsv1.NamespaceScoped},
		{gvk.CertManagerClusterIssuer, "clusterissuers", "clusterissuer", apiextensionsv1.ClusterScoped},
	}

	crds := make([]*apiextensionsv1.CustomResourceDefinition, 0, len(defs))
	for _, d := range defs {
		crd, err := et.RegisterCRD(ctx, d.gvkDef, d.plural, d.singular, d.scope, opts...)
		if err != nil {
			return crds, err
		}
		crds = append(crds, crd)
	}
	return crds, nil
}
