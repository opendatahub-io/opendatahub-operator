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
	"strings"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	resourcespredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/env"
)

// caRootDuration is the validity period of the root CA certificate (~100 years).
// No automatic renewal is configured; a renewal strategy is tracked as a follow-up.
const caRootDuration = "876000h"

// These CRD names must be covered consistently by MonitorCRDs and CRDPredicate.
// If a CRD is added here, update both functions.
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

	EnvOperatorWebhookCertSecretName = "RHAI_WEBHOOK_CERT_SECRET_NAME" //nolint:gosec
	EnvOperatorWebhookServiceName    = "RHAI_WEBHOOK_SERVICE_NAME"
	EnvOperatorWebhookCertName       = "RHAI_WEBHOOK_CERT_NAME"
)

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
func WithOperatorCert(namespace string) BootstrapConfigOpt {
	return func(c *BootstrapConfig) {
		c.OperatorCertConfig = BootstrapOperatorCertConfig(namespace)
	}
}

// DefaultBootstrapConfig returns the standard ODH PKI bootstrap configuration.
// Overridable fields (CAIssuerName, CertName, CertManagerNamespace) are resolved
// from RHAI_* environment variables, falling back to hardcoded defaults.
//
// By default, Operator is nil and no webhook Certificate is created.
// Use [WithOperatorCert] to enable the operator webhook certificate.
func DefaultBootstrapConfig(opts ...BootstrapConfigOpt) BootstrapConfig {
	prefix := "opendatahub"

	config := BootstrapConfig{
		// Not overridable: internal bootstrap detail, not referenced by downstream components.
		IssuerName:           prefix + "-selfsigned-issuer",
		CertName:             env.GetOrDefault(EnvCertName, prefix+"-ca"),
		CertManagerNamespace: env.GetOrDefault(EnvCertManagerNS, "cert-manager"),
		CAIssuerName:         env.GetOrDefault(EnvCAIssuerName, prefix+"-ca-issuer"),
	}
	for _, opt := range opts {
		opt(&config)
	}
	return config
}

// BootstrapOperatorCertConfig returns the default operator webhook certificate configuration,
// reading overrides from environment variables.
func BootstrapOperatorCertConfig(namespace string) *OperatorCertConfig {
	prefix := "opendatahub"

	return &OperatorCertConfig{
		Namespace:             namespace,
		WebhookCertName:       env.GetOrDefault(EnvOperatorWebhookCertName, prefix+"-operator-webhook-cert"),
		WebhookCertSecretName: env.GetOrDefault(EnvOperatorWebhookCertSecretName, prefix+"-operator-controller-webhook-cert"),
		WebhookServiceName:    env.GetOrDefault(EnvOperatorWebhookServiceName, prefix+"-operator-webhook-service"),
	}
}

// NewBootstrapAction returns a reusable pipeline action that adds the cert-manager PKI trust
// chain resources to the reconciliation request for deployment by the pipeline's deploy action:
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

// createRootCACertificate returns the root CA Certificate issued by the self-signed ClusterIssuer.
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

// NewCleanupAction returns a finalizer action that explicitly deletes CertManager/cluster CR.
//
// When deleting the AKE/CWE CR, we need to explicitly delete CertManager/cluster first.
// The cert-manager-operator has finalizers on CertManager/cluster that clean up its Deployments,
// but these finalizers won't run if the operator is killed before the CR is deleted.
// By deleting CertManager/cluster in this action, the operator stays alive long enough
// to process its finalizers and clean up properly before the cascade deletion continues.
//
// The action does nothing if CertManager/cluster does not exist or if it is not owned
// by the current CR instance.
func NewCleanupAction() actions.Fn {
	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		l := logf.FromContext(ctx)

		cm := &unstructured.Unstructured{}
		cm.SetGroupVersionKind(gvk.CertManagerV1Alpha1)

		if err := rr.Client.Get(ctx, client.ObjectKey{Name: "cluster"}, cm); err != nil {
			if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
				return nil
			}
			return err
		}

		// Only delete CertManager/cluster if this CR instance owns it.
		owned := false
		for _, ref := range cm.GetOwnerReferences() {
			if ref.UID == rr.Instance.GetUID() {
				owned = true
				break
			}
		}
		if !owned {
			l.V(1).Info("CertManager/cluster is not owned by this instance, skipping cleanup",
				"instance", rr.Instance.GetName())
			return nil
		}

		// Trigger deletion if not already in progress.
		if cm.GetDeletionTimestamp().IsZero() {
			l.Info("deleting CertManager/cluster to allow cert-manager-operator to clean up operands",
				"instance", rr.Instance.GetName())
			if err := rr.Client.Delete(ctx, cm); err != nil && !k8serr.IsNotFound(err) {
				return err
			}
		}

		// Only wait for cert-manager-operator's own finalizers (identified by its domain prefix).
		var operatorFinalizers []string
		for _, f := range cm.GetFinalizers() {
			if strings.HasPrefix(f, "cert-manager-operator.") {
				operatorFinalizers = append(operatorFinalizers, f)
			}
		}
		if len(operatorFinalizers) == 0 {
			l.V(1).Info("CertManager/cluster has no remaining cert-manager-operator finalizers, deletion complete",
				"instance", rr.Instance.GetName())
			return nil
		}

		// The CR is still Terminating and cert-manager-operator finalizers remain.
		// Return an error to trigger reconciler re-queue. On the next call, Get should
		// return NotFound once cert-manager-operator has removed all its finalizers.
		l.Info("waiting for CertManager/cluster to be fully deleted",
			"instance", rr.Instance.GetName(),
			"remainingFinalizers", cm.GetFinalizers())
		return fmt.Errorf("waiting for CertManager/cluster to be fully deleted (remaining finalizers: %v)",
			cm.GetFinalizers())
	}
}

// MonitorCRDs returns a dependency.ActionOpts that checks whether the three core cert-manager
// CRDs (Certificate, Issuer, ClusterIssuer) are registered on the cluster. If any CRD
// is absent, DependenciesAvailable is set to False.
func MonitorCRDs() dependency.ActionOpts {
	return dependency.Combine(
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerCertificate}),
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerIssuer}),
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerClusterIssuer}),
	)
}

// CRDPredicate returns a predicate that matches CustomResourceDefinition events for
// the three core cert-manager CRDs.
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
//   - a cert-manager CRDs watch to trigger reconciliation when cert-manager is installed,
//   - explicit watches for the PKI resource instances (ClusterIssuers, Certificate)
//     so the controller reconciles when they are modified or deleted,
//   - a monitoring action to set the DependenciesAvailable condition,
//   - a bootstrap action to deploy the PKI trust chain,
//   - a condition to set the DependenciesAvailable status.
//
// instanceName is the controller's singleton instance name, used to route CRD watch events
// to the correct reconciler queue via handlers.ToNamed.
//
// [BootstrapConfig] is the configuration for the cert-manager PKI trust chain.
//
// Use with [reconciler.ComposeWith]:
//
//	b.ComposeWith(certmanager.Bootstrap[*MyControllerType](instanceName, certmanager.DefaultBootstrapConfig()))
func Bootstrap[T common.PlatformObject](instanceName string, config BootstrapConfig) func(*reconciler.ReconcilerBuilder[T]) {
	return func(b *reconciler.ReconcilerBuilder[T]) {
		certPredicates := []predicate.Predicate{
			resourcespredicates.CreatedOrUpdatedOrDeletedNamedInNamespace(config.CertName, config.CertManagerNamespace),
		}
		if config.OperatorCertConfig != nil {
			certPredicates = append(certPredicates,
				resourcespredicates.CreatedOrUpdatedOrDeletedNamedInNamespace(
					config.OperatorCertConfig.WebhookCertName, config.OperatorCertConfig.Namespace),
			)
		}

		b.Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(handlers.ToNamed(instanceName)),
			reconciler.WithPredicates(CRDPredicate()),
		).
			WatchesGVK(gvk.CertManagerClusterIssuer,
				reconciler.WithEventHandler(handlers.ToNamed(instanceName)),
				reconciler.WithPredicates(predicate.Or(
					resourcespredicates.CreatedOrUpdatedOrDeletedNamed(config.IssuerName),
					resourcespredicates.CreatedOrUpdatedOrDeletedNamed(config.CAIssuerName),
				)),
				reconciler.Dynamic(reconciler.CrdExists(gvk.CertManagerClusterIssuer)),
			).
			WatchesGVK(gvk.CertManagerCertificate,
				reconciler.WithEventHandler(handlers.ToNamed(instanceName)),
				reconciler.WithPredicates(predicate.Or(certPredicates...)),
				reconciler.Dynamic(reconciler.CrdExists(gvk.CertManagerCertificate)),
			).
			WatchesGVK(gvk.CertManagerV1Alpha1,
				reconciler.WithEventHandler(handlers.ToNamed(instanceName)),
				reconciler.WithPredicates(resourcespredicates.CreatedOrUpdatedOrDeletedNamed("cluster")),
				reconciler.Dynamic(reconciler.CrdExists(gvk.CertManagerV1Alpha1)),
			).
			WithAction(dependency.NewAction(MonitorCRDs())).
			WithActionE(NewBootstrapAction(config)).
			WithConditions(status.ConditionDependenciesAvailable).
			WithFinalizer(NewCleanupAction())
	}
}
