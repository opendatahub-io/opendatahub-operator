package gateway

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
)

// requireCertManager verifies the cert-manager API required by XKS gateway
// certificates is available. XKS declares cert-manager as a required
// dependency, so silently falling back to an operator-generated certificate
// would hide a broken platform installation and bypass certificate renewal.
func requireCertManager(ctx context.Context, cli client.Client) error {
	hasCertificate, err := cluster.HasCRD(ctx, cli, gvk.CertManagerCertificate)
	if err != nil {
		return fmt.Errorf("failed to check cert-manager Certificate CRD presence: %w", err)
	}
	if !hasCertificate {
		return errors.New("cert-manager Certificate CRD is required on XKS")
	}
	return nil
}

// resolveIssuerRef returns the cert-manager issuer name and kind used to sign gateway certificates.
//
// Precedence (highest first):
//  1. per-GatewayConfig spec.certificate.issuerRef (multi-tenant override)
//  2. RHAI_ISSUER_REF_* environment variables (platform-wide default, set per build/platform)
//  3. hardcoded defaults from certmanager.DefaultBootstrapConfig
//
// The env-based default is the same one consumed by the cert-manager bootstrap and module
// platform config, so a single GatewayConfig (the current singleton) resolves to the platform
// issuer (e.g. rhai-ca-issuer on RHOAI) with no per-CR configuration required.
func resolveIssuerRef(cert *infrav1.CertificateSpec) (string, string) {
	bootstrapConfig := certmanager.DefaultBootstrapConfig()
	name := bootstrapConfig.CAIssuerName
	kind := bootstrapConfig.IssuerRefKind

	if cert != nil && cert.IssuerRef != nil {
		if cert.IssuerRef.Name != "" {
			name = cert.IssuerRef.Name
		}
		if cert.IssuerRef.Kind != "" {
			kind = cert.IssuerRef.Kind
		}
	}

	return name, kind
}

// buildCertManagerCertificate builds a cert-manager Certificate CR that instructs cert-manager to
// issue (and auto-renew) a TLS Secret named secretName, signed by the referenced issuer.
//
// The returned object is meant to be added to the reconciliation request so the reconciler manages
// its lifecycle (owner references, cleanup) via the gateway controller's owned Certificate GVK.
func buildCertManagerCertificate(name, namespace, secretName string, dnsNames []string, issuerName, issuerKind string) (*unstructured.Unstructured, error) {
	dns := make([]any, len(dnsNames))
	for i, d := range dnsNames {
		dns[i] = d
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerCertificate)
	u.SetName(name)
	u.SetNamespace(namespace)

	if err := unstructured.SetNestedMap(u.Object, map[string]any{
		"secretName": secretName,
		"dnsNames":   dns,
		"issuerRef": map[string]any{
			"name":  issuerName,
			"kind":  issuerKind,
			"group": gvk.CertManagerCertificate.Group,
		},
	}, "spec"); err != nil {
		return nil, fmt.Errorf("failed to set spec on Certificate %s/%s: %w", namespace, name, err)
	}

	return u, nil
}
