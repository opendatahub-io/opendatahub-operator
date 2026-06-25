package modules

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

var certManagerPrerequisiteCRDs = []schema.GroupVersionKind{
	gvk.CertManagerCertificate,
	gvk.CertManagerCertificateRequest,
	gvk.CertManagerIssuer,
}

// certManagerCRDsAvailable reports whether the cert-manager API types required
// for webhook TLS provisioning are registered on the cluster.
func certManagerCRDsAvailable(ctx context.Context, cli client.Client) (bool, error) {
	for _, crdGVK := range certManagerPrerequisiteCRDs {
		available, err := cluster.HasCRD(ctx, cli, crdGVK)
		if err != nil {
			return false, fmt.Errorf("checking cert-manager CRD %s: %w", crdGVK.Kind, err)
		}
		if !available {
			return false, nil
		}
	}
	return true, nil
}
