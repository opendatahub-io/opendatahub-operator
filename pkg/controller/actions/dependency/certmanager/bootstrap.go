// This file bootstraps the cert-manager PKI trust chain required by downstream components.
// The chain consists of three cert-manager resources:
//
//   - Self-signed ClusterIssuer (opendatahub-selfsigned-issuer): a bootstrap issuer
//     with no upstream authority. Its sole purpose is to sign the root CA certificate.
//
//   - Root CA Certificate (opendatahub-ca): a certificate marked isCA=true, issued by
//     the self-signed issuer. cert-manager stores the resulting key and certificate in
//     a Secret of the same name in the cert-manager namespace.
//
//   - CA-backed ClusterIssuer (opendatahub-ca-issuer): references the Secret produced
//     by the root CA Certificate. Downstream components (e.g. KServe) use this issuer
//     to request their own leaf certificates, so they trust the same root CA.

package certmanager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// caRootDuration is the validity period of the opendatahub-ca root CA certificate.
// This value is intentionally long; the appropriate validity, key algorithm, and renewal
// strategy will be defined in future changes. cert-manager schedules renewal
// at 2/3 of this duration by default.
const caRootDuration = "876000h"

// BootstrapConfig holds the resource names that form the cert-manager PKI trust chain.
// The names are part of the API contract between the operator and downstream components
// (for example, KServe references opendatahub-ca-issuer by name). Use DefaultBootstrapConfig
// in production; override only for testing or non-standard cert-manager installations.
type BootstrapConfig struct {
	// IssuerName is the name of the self-signed ClusterIssuer used to bootstrap the root CA certificate.
	IssuerName string

	// CertName is the name of the root CA Certificate and the Secret that cert-manager creates for it.
	CertName string

	// CertManagerNamespace is the namespace where cert-manager is installed. The root CA
	// Certificate is created here so that cert-manager places the resulting Secret in this
	// namespace, where the CA-backed ClusterIssuer can find it. cert-manager ClusterIssuers
	// can only reference Secrets in the cert-manager controller's own namespace.
	CertManagerNamespace string

	// CAIssuerName is the name of the CA-backed ClusterIssuer. Downstream components use this
	// issuer to request their own leaf certificates.
	CAIssuerName string
}

// DefaultBootstrapConfig returns the standard ODH PKI bootstrap configuration.
// These names are part of the API contract with downstream components.
func DefaultBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		IssuerName:           "opendatahub-selfsigned-issuer",
		CertName:             "opendatahub-ca",
		CertManagerNamespace: "cert-manager",
		CAIssuerName:         "opendatahub-ca-issuer",
	}
}

// NewBootstrapAction returns a reusable pipeline action that adds the cert-manager PKI trust
// chain resources to the reconciliation request for deployment by the pipeline's deploy action.
// The chain consists of a self-signed ClusterIssuer (config.IssuerName), a root CA Certificate
// (config.CertName) issued by it, and a CA-backed ClusterIssuer (config.CAIssuerName) that
// downstream components use to request leaf certificates.
//
// The action is a no-op when cert-manager CRDs (ClusterIssuer or Certificate) are absent on the cluster.
// Pair this action with MonitorCRDs() earlier in the pipeline to surface missing CRDs to users
// via the DependenciesAvailable condition.
//
// Resources are owned and garbage-collected by the wiring controller's standard deploy and
// gc pipeline actions, following the same lifecycle as all other ODH-managed resources.
func NewBootstrapAction(config BootstrapConfig) actions.Fn {
	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		hasClusterIssuer, err := cluster.HasCRD(ctx, rr.Client, gvk.CertManagerClusterIssuer)
		if err != nil {
			return fmt.Errorf("failed to check cert-manager ClusterIssuer CRD presence: %w", err)
		}
		hasCertificate, err := cluster.HasCRD(ctx, rr.Client, gvk.CertManagerCertificate)
		if err != nil {
			return fmt.Errorf("failed to check cert-manager Certificate CRD presence: %w", err)
		}
		if !hasClusterIssuer || !hasCertificate {
			return nil
		}

		issuer, err := selfSignedIssuer(config)
		if err != nil {
			return err
		}

		caCert, err := rootCACertificate(config)
		if err != nil {
			return err
		}

		caIssuer, err := caBackedIssuer(config)
		if err != nil {
			return err
		}

		return rr.AddResources(issuer, caCert, caIssuer)
	}
}

// selfSignedIssuer returns the bootstrap ClusterIssuer that signs the root CA certificate.
func selfSignedIssuer(config BootstrapConfig) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
	u.SetName(config.IssuerName)
	if err := unstructured.SetNestedMap(u.Object, map[string]any{
		"selfSigned": map[string]any{},
	}, "spec"); err != nil {
		return nil, fmt.Errorf("failed to set spec on self-signed ClusterIssuer: %w", err)
	}
	return u, nil
}

// rootCACertificate returns the root CA Certificate resource. cert-manager processes this
// Certificate and places the resulting CA key and certificate in a Secret of the same name
// in config.CertManagerNamespace, where caBackedIssuer can reference it.
func rootCACertificate(config BootstrapConfig) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerCertificate)
	u.SetName(config.CertName)
	u.SetNamespace(config.CertManagerNamespace)
	if err := unstructured.SetNestedMap(u.Object, map[string]any{
		"isCA":       true,
		"commonName": config.CertName,
		"secretName": config.CertName,
		"duration":   caRootDuration,
		// Explicit usages override cert-manager's leaf-oriented defaults
		// (digital signature + key encipherment). A root CA only needs cert sign.
		"usages": []any{"cert sign"},
		"issuerRef": map[string]any{
			"name":  config.IssuerName,
			"kind":  gvk.CertManagerClusterIssuer.Kind,
			"group": gvk.CertManagerClusterIssuer.Group,
		},
	}, "spec"); err != nil {
		return nil, fmt.Errorf("failed to set spec on root CA Certificate: %w", err)
	}
	return u, nil
}

// caBackedIssuer returns the CA-backed ClusterIssuer that downstream components use to request
// leaf certificates. It references the Secret created by the root CA Certificate.
func caBackedIssuer(config BootstrapConfig) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
	u.SetName(config.CAIssuerName)
	if err := unstructured.SetNestedMap(u.Object, map[string]any{
		"ca": map[string]any{
			"secretName": config.CertName,
		},
	}, "spec"); err != nil {
		return nil, fmt.Errorf("failed to set spec on CA-backed ClusterIssuer: %w", err)
	}
	return u, nil
}
