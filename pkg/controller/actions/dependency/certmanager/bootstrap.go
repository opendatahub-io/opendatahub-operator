// This file bootstraps the cert-manager PKI trust chain.
// The chain consists of three cert-manager resources:
//
//   - Self-signed ClusterIssuer: a bootstrap issuer with no external signing authority.
//     Its only job is to sign the root CA certificate.
//
//   - Root CA Certificate: a CA certificate (isCA=true), issued by the self-signed issuer.
//
//   - CA-backed ClusterIssuer: references the Secret that cert-manager creates for the
//     root CA Certificate. Other components use this issuer to get their own certificates.
//
// When the operator namespace is configured, the bootstrap
// also creates a webhook serving Certificate issued by the CA-backed ClusterIssuer.

package certmanager

import (
	"context"
	"errors"
	"fmt"
	"os"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// caRootDuration is the validity period of the root CA certificate.
// This value is intentionally long. The renewal strategy will be defined in future changes.
const caRootDuration = "876000h"

// Core cert-manager CRD resource names.
const (
	certManagerCertificateCRD   = "certificates.cert-manager.io"
	certManagerIssuerCRD        = "issuers.cert-manager.io"
	certManagerClusterIssuerCRD = "clusterissuers.cert-manager.io"
)

// DefaultIssuerRefKind is the default issuer reference kind used by downstream components.
const DefaultIssuerRefKind = "ClusterIssuer"

// Environment variable names for overriding the default cert-manager PKI configuration.
// These are used by the operator and downstream components (e.g. KServe params.env injection)
// to allow external PKI (e.g. cloud controller manager) without code changes.
const (
	EnvCAIssuerName    = "RHAI_ISSUER_REF_NAME"
	EnvIssuerRefKind   = "RHAI_ISSUER_REF_KIND"
	EnvCertName        = "RHAI_CA_SECRET_NAME"
	EnvCertManagerNS   = "RHAI_CA_SECRET_NAMESPACE"
	EnvIstioCACertPath = "RHAI_ISTIO_CA_CERTIFICATE_PATH"

	EnvOperatorNamespace             = "RHAI_OPERATOR_NAMESPACE"
	EnvOperatorWebhookCertSecretName = "RHAI_WEBHOOK_CERT_SECRET_NAME" //nolint:gosec
	EnvOperatorWebhookServiceName    = "RHAI_WEBHOOK_SERVICE_NAME"
)

// EnvOrDefault returns the value of the named environment variable or fallback if unset/empty.
func EnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var OperatorNamespace = os.Getenv(EnvOperatorNamespace)

// OperatorCertConfig groups the configuration for the operator's webhook serving certificate.
// When Namespace is empty, no webhook Certificate is created.
type OperatorCertConfig struct {
	// Namespace is the namespace where the operator is deployed.
	// When empty, no webhook Certificate is created.
	Namespace string

	// WebhookCertName is the name of the cert-manager Certificate resource for the webhook.
	WebhookCertName string

	// WebhookCertSecretName is the name of the Secret that cert-manager populates
	// with the webhook TLS certificate. Must match the operator deployment's volume mount.
	WebhookCertSecretName string

	// WebhookServiceName is the name of the webhook Service, used to construct DNS SANs.
	WebhookServiceName string
}

// BootstrapConfig holds the resource names to create a PKI trust chain using cert-manager.
// [DefaultBootstrapConfig] populates these from RHAI_* env vars (falling back to hardcoded defaults).
// Functional options can override individual fields for testing.
type BootstrapConfig struct {
	// IssuerName is the name of the self-signed ClusterIssuer used to bootstrap the root CA certificate.
	IssuerName string

	// CertName is the name of the root CA Certificate and the Secret that cert-manager creates for it.
	CertName string

	// CertManagerNamespace is the namespace where cert-manager is installed.
	// The root CA Certificate is created in this namespace.
	CertManagerNamespace string

	// CAIssuerName is the name of the CA-backed ClusterIssuer. Other components use this
	// issuer to get their own certificates.
	CAIssuerName string

	// OperatorCertConfig holds the configuration for the operator's webhook serving certificate.
	// When OperatorCertConfig.Namespace is empty, no webhook Certificate is created.
	OperatorCertConfig *OperatorCertConfig
}

// BootstrapConfigOpt is a functional option for [DefaultBootstrapConfig].
type BootstrapConfigOpt func(*BootstrapConfig)

// WithOperatorCert enables creation of the operator's webhook serving Certificate
// using the defaults from DefaultOperatorCertConfig.
func WithOperatorCert() BootstrapConfigOpt {
	return func(c *BootstrapConfig) {
		c.OperatorCertConfig = BootstrapOperatorCertConfig()
	}
}

// DefaultBootstrapConfig returns the standard ODH PKI bootstrap configuration.
// Overridable fields (CAIssuerName, CertName, CertManagerNamespace) are resolved
// from RHAI_* environment variables, falling back to hardcoded defaults.
//
// By default, Operator is nil and no webhook Certificate is created.
// Use [WithOperatorCert] to enable the operator webhook certificate.
func DefaultBootstrapConfig(opts ...BootstrapConfigOpt) BootstrapConfig {
	config := BootstrapConfig{
		// Not overridable: internal bootstrap detail, not referenced by downstream components.
		IssuerName:           "opendatahub-selfsigned-issuer",
		CertName:             EnvOrDefault(EnvCertName, "opendatahub-ca"),
		CertManagerNamespace: EnvOrDefault(EnvCertManagerNS, "cert-manager"),
		CAIssuerName:         EnvOrDefault(EnvCAIssuerName, "opendatahub-ca-issuer"),
	}
	for _, opt := range opts {
		opt(&config)
	}
	return config
}

// BootstrapOperatorCertConfig returns the default operator webhook certificate configuration,
// reading overrides from environment variables. The caller must set Namespace (or use
// RHAI_OPERATOR_NAMESPACE) before attaching it to a BootstrapConfig.
func BootstrapOperatorCertConfig() *OperatorCertConfig {
	return &OperatorCertConfig{
		Namespace:             OperatorNamespace,
		WebhookCertName:       "opendatahub-operator-webhook-cert",
		WebhookCertSecretName: EnvOrDefault(EnvOperatorWebhookCertSecretName, "opendatahub-operator-controller-webhook-cert"),
		WebhookServiceName:    EnvOrDefault(EnvOperatorWebhookServiceName, "opendatahub-operator-webhook-service"),
	}
}

// NewBootstrapAction returns a reusable pipeline action that adds the cert-manager PKI trust
// chain resources to the reconciliation request for deployment by the pipeline's deploy action.
//
// The chain consists of:
//
// - a self-signed ClusterIssuer
// - a root CA Certificate
// - a CA-backed ClusterIssuer
// - (optional) a webhook serving Certificate, when Operator.Namespace is set
//
// The action is a no-op when cert-manager CRDs (ClusterIssuer or Certificate) are absent on the cluster.
func NewBootstrapAction(config BootstrapConfig) (actions.Fn, error) {
	if config.OperatorCertConfig != nil && config.OperatorCertConfig.Namespace == "" {
		return nil, errors.New("operator namespace must not be empty when operator cert config generation is set")
	}

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

		issuer, err := createSelfSignedIssuer(config)
		if err != nil {
			return err
		}

		caCert, err := createRootCACertificate(config)
		if err != nil {
			return err
		}

		caIssuer, err := createCABackedIssuer(config)
		if err != nil {
			return err
		}

		resources := []client.Object{issuer, caCert, caIssuer}

		if config.OperatorCertConfig != nil {
			webhookCert, err := createWebhookCertificate(config)
			if err != nil {
				return err
			}
			resources = append(resources, webhookCert)
		}

		return rr.AddResources(resources...)
	}, nil
}

// createSelfSignedIssuer returns the bootstrap ClusterIssuer that signs the root CA certificate.
func createSelfSignedIssuer(config BootstrapConfig) (*unstructured.Unstructured, error) {
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

func createRootCACertificate(config BootstrapConfig) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerCertificate)
	u.SetName(config.CertName)
	u.SetNamespace(config.CertManagerNamespace)
	if err := unstructured.SetNestedMap(u.Object, map[string]any{
		"isCA":       true,
		"commonName": config.CertName,
		"secretName": config.CertName,
		"duration":   caRootDuration,
		// cert-manager's default usages include key encipherment, which a CA does not need.
		// Only cert sign is required here.
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

// createWebhookCertificate returns a cert-manager Certificate for the operator's webhook
// serving TLS. It is issued by the CA-backed ClusterIssuer from the bootstrap chain.
func createWebhookCertificate(config BootstrapConfig) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerCertificate)
	u.SetName(config.OperatorCertConfig.WebhookCertName)
	u.SetNamespace(config.OperatorCertConfig.Namespace)
	if err := unstructured.SetNestedMap(u.Object, map[string]any{
		"secretName": config.OperatorCertConfig.WebhookCertSecretName,
		"dnsNames": []any{
			fmt.Sprintf("%s.%s.svc", config.OperatorCertConfig.WebhookServiceName, config.OperatorCertConfig.Namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", config.OperatorCertConfig.WebhookServiceName, config.OperatorCertConfig.Namespace),
		},
		"issuerRef": map[string]any{
			"name":  config.CAIssuerName,
			"kind":  gvk.CertManagerClusterIssuer.Kind,
			"group": gvk.CertManagerClusterIssuer.Group,
		},
	}, "spec"); err != nil {
		return nil, fmt.Errorf("failed to set spec on webhook Certificate: %w", err)
	}
	return u, nil
}

// createCABackedIssuer returns the CA-backed ClusterIssuer that other components use to request
// certificates. It references the Secret created by the root CA Certificate.
func createCABackedIssuer(config BootstrapConfig) (*unstructured.Unstructured, error) {
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

// MonitorCRDs returns a dependency.ActionOpts that checks whether the three core cert-manager
// CRDs (Certificate, Issuer, ClusterIssuer) are registered on the cluster. If any CRD
// is absent, DependenciesAvailable is set to False.
//
// Must cover exactly the same CRDs as CRDPredicate. If a CRD is added here, add the
// corresponding name to the constants block above and update CRDPredicate.
func MonitorCRDs() dependency.ActionOpts {
	return dependency.Combine(
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerCertificate}),
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerIssuer}),
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerClusterIssuer}),
	)
}

// CRDPredicate returns a predicate that matches CustomResourceDefinition events for
// the three core cert-manager CRDs.
//
// Must cover exactly the same CRDs as MonitorCRDs. If a CRD is added to MonitorCRDs,
// add the corresponding name to the constants block above and update this predicate.
func CRDPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj.GetName() {
		case certManagerCertificateCRD, certManagerIssuerCRD, certManagerClusterIssuerCRD:
			return true
		}
		return false
	})
}

// Bootstrap returns a builder configurator that registers all cert-manager bootstrapping
// concerns onto the builder:
//
// - a cert-manager CRDs watch to trigger reconciliation,
// - a monitoring action to set the DependenciesAvailable condition,
// - a bootstrap action to deploy the PKI trust chain,
// - a condition to set the DependenciesAvailable status.
//
// instanceName is the controller's singleton instance name, used to route CRD watch events
// to the correct reconciler queue via handlers.ToNamed.
//
// [BootstrapConfig] is the configuration for the cert-manager PKI trust chain.
//
// Use with [reconciler.ComposeWith]. T must be supplied explicitly because Go cannot infer
// it from the function arguments:
//
//	b.ComposeWith(certmanager.Bootstrap[*MyControllerType](instanceName, certmanager.DefaultBootstrapConfig()))
func Bootstrap[T common.PlatformObject](instanceName string, config BootstrapConfig) func(*reconciler.ReconcilerBuilder[T]) {
	return func(b *reconciler.ReconcilerBuilder[T]) {
		b.Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(handlers.ToNamed(instanceName)),
			reconciler.WithPredicates(CRDPredicate()),
		).
			WithAction(dependency.NewAction(MonitorCRDs())).
			WithActionE(NewBootstrapAction(config)).
			WithConditions(status.ConditionDependenciesAvailable)
	}
}
