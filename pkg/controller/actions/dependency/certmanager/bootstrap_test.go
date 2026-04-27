package certmanager_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestDefaultBootstrapConfigEnvOverrides verifies that DefaultBootstrapConfig honors
// RHAI_* environment variable overrides for each overridable field.
func TestDefaultBootstrapConfigEnvOverrides(t *testing.T) {
	cases := []struct {
		name     string
		envVar   string
		envValue string
		getField func(certmanager.BootstrapConfig) string
		expected string
	}{
		{
			name:     "defaults CAIssuerName when env var is unset",
			envVar:   "",
			getField: func(c certmanager.BootstrapConfig) string { return c.CAIssuerName },
			expected: "opendatahub-ca-issuer",
		},
		{
			name:     "defaults CertName when env var is unset",
			envVar:   "",
			getField: func(c certmanager.BootstrapConfig) string { return c.CertName },
			expected: "opendatahub-ca",
		},
		{
			name:     "defaults CertManagerNamespace when env var is unset",
			envVar:   "",
			getField: func(c certmanager.BootstrapConfig) string { return c.CertManagerNamespace },
			expected: "cert-manager",
		},
		{
			name:     "overrides CAIssuerName from RHAI_ISSUER_REF_NAME",
			envVar:   certmanager.EnvCAIssuerName,
			envValue: "custom-ca-issuer",
			getField: func(c certmanager.BootstrapConfig) string { return c.CAIssuerName },
			expected: "custom-ca-issuer",
		},
		{
			name:     "overrides CertName from RHAI_CA_SECRET_NAME",
			envVar:   certmanager.EnvCertName,
			envValue: "custom-ca-cert",
			getField: func(c certmanager.BootstrapConfig) string { return c.CertName },
			expected: "custom-ca-cert",
		},
		{
			name:     "overrides CertManagerNamespace from RHAI_CA_SECRET_NAMESPACE",
			envVar:   certmanager.EnvCertManagerNS,
			envValue: "custom-cert-ns",
			getField: func(c certmanager.BootstrapConfig) string { return c.CertManagerNamespace },
			expected: "custom-cert-ns",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			// t.Setenv("") is preferred over os.Unsetenv because the usetesting linter
			// forbids os.Setenv/os.Unsetenv in tests. EnvOrDefault uses os.Getenv, which
			// returns "" for both unset and empty, so the behavior is identical.
			t.Setenv(certmanager.EnvCAIssuerName, "")
			t.Setenv(certmanager.EnvCertName, "")
			t.Setenv(certmanager.EnvCertManagerNS, "")

			if tc.envVar != "" {
				t.Setenv(tc.envVar, tc.envValue)
			}

			config := certmanager.DefaultBootstrapConfig()
			g.Expect(tc.getField(config)).To(Equal(tc.expected))
		})
	}

	t.Run("overrides all fields simultaneously", func(t *testing.T) {
		g := NewWithT(t)

		t.Setenv(certmanager.EnvCAIssuerName, "all-ca-issuer")
		t.Setenv(certmanager.EnvCertName, "all-ca-cert")
		t.Setenv(certmanager.EnvCertManagerNS, "all-cert-ns")

		config := certmanager.DefaultBootstrapConfig()
		g.Expect(config.CAIssuerName).To(Equal("all-ca-issuer"))
		g.Expect(config.CertName).To(Equal("all-ca-cert"))
		g.Expect(config.CertManagerNamespace).To(Equal("all-cert-ns"))
	})

	t.Run("functional options take precedence over env vars", func(t *testing.T) {
		g := NewWithT(t)

		t.Setenv(certmanager.EnvCAIssuerName, "")
		t.Setenv(certmanager.EnvCertName, "")
		t.Setenv(certmanager.EnvCertManagerNS, "")

		t.Setenv(certmanager.EnvCAIssuerName, "env-ca-issuer")

		config := certmanager.DefaultBootstrapConfig(func(c *certmanager.BootstrapConfig) {
			c.CAIssuerName = "opt-ca-issuer"
		})
		g.Expect(config.CAIssuerName).To(Equal("opt-ca-issuer"))
	})
}

// createBootstrapCRDs registers the three cert-manager CRDs required by the bootstrap action
// and schedules their cleanup for the end of the test.
func createBootstrapCRDs(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
	t.Helper()
	crds, err := envTest.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	for _, crd := range crds {
		envt.CleanupDelete(t, g, ctx, envTest.Client(), crd)
	}
}

// createBootstrapNamespace ensures the given namespace exists in the cluster, ignoring
// AlreadyExists errors so the function is safe to call across subtests sharing an envtest.
func createBootstrapNamespace(t *testing.T, g *WithT, ctx context.Context, cli client.Client, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := cli.Create(ctx, ns)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		g.Expect(err).NotTo(HaveOccurred())
	}
}

// getClusterIssuer fetches a cert-manager ClusterIssuer by name.
func getClusterIssuer(ctx context.Context, cli client.Client, name string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
	return u, cli.Get(ctx, client.ObjectKey{Name: name}, u)
}

// getRootCACertificate fetches the root CA cert-manager Certificate by name and namespace.
func getRootCACertificate(ctx context.Context, cli client.Client, name, namespace string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerCertificate)
	return u, cli.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, u)
}

// getWebhookCertificate fetches the webhook cert-manager Certificate by name and namespace.
func getWebhookCertificate(ctx context.Context, cli client.Client, name, namespace string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.CertManagerCertificate)
	return u, cli.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, u)
}

// TestBootstrapCertManagerPKI verifies that NewBootstrapAction adds the cert-manager PKI trust
// chain resources to the reconciliation request, which are then applied by the deploy action.
//
// The "absent CRDs" subtest uses its own envtest instance because HasCRD relies on the REST
// mapper, whose discovery cache refreshes asynchronously after CRD deletion. A shared instance
// cannot guarantee zero CRDs are visible at the start of that case when other subtests registered
// CRDs beforehand. All remaining subtests share a single envtest — CRDs are registered once and
// never removed, so the REST mapper cache is not a concern.
func TestBootstrapCertManagerPKI(t *testing.T) {
	config := certmanager.DefaultBootstrapConfig()

	t.Run("no-op when cert-manager CRDs are absent", func(t *testing.T) {
		g := NewWithT(t)

		envTest, err := envt.New()
		g.Expect(err).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = envTest.Stop() })

		ctx := context.Background()
		rr := &types.ReconciliationRequest{Client: envTest.Client()}

		action, err := certmanager.NewBootstrapAction(config)
		g.Expect(err).NotTo(HaveOccurred())
		err = action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rr.Resources).To(BeEmpty(), "no resources should be queued when CRDs are absent")
	})

	// All remaining subtests share one envtest. CRDs are registered once and never removed.
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	createBootstrapCRDs(t, g, ctx, envTest)
	createBootstrapNamespace(t, g, ctx, cli, config.CertManagerNamespace)

	// instance and controller are required by the deploy action. Any component CR registered
	// in the scheme works for instance; the controller mock returns false for Owns so no
	// controller owner references are set on the PKI resources.
	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	controller := mocks.NewMockController(func(m *mocks.MockController) {
		m.On("Owns", mock.Anything).Return(false)
	})

	bootstrapAction, err := certmanager.NewBootstrapAction(config)
	g.Expect(err).NotTo(HaveOccurred())
	deployAction := deploy.NewAction(deploy.WithFieldOwner("test-certmanager-bootstrap"))

	// runPipeline creates a fresh reconciliation request and runs bootstrap followed by deploy.
	// A fresh request is used on each call to avoid accumulating resources across pipeline runs.
	runPipeline := func() error {
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Controller: controller}
		if err := bootstrapAction(ctx, rr); err != nil {
			return err
		}
		return deployAction(ctx, rr)
	}

	// Initial run: create all three PKI resources before the subtests execute.
	g.Expect(runPipeline()).NotTo(HaveOccurred())

	t.Run("creates all three PKI resources when CRDs are present", func(t *testing.T) {
		g := NewWithT(t)

		// Assert self-signed ClusterIssuer was created with the selfSigned spec.
		issuer, err := getClusterIssuer(ctx, cli, config.IssuerName)
		g.Expect(err).NotTo(HaveOccurred())
		_, found, err := unstructured.NestedMap(issuer.Object, "spec", "selfSigned")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue(), "spec.selfSigned should exist on the self-signed ClusterIssuer")

		// Assert root CA Certificate was created with the correct spec fields.
		cert, err := getRootCACertificate(ctx, cli, config.CertName, config.CertManagerNamespace)
		g.Expect(err).NotTo(HaveOccurred())
		isCA, _, err := unstructured.NestedBool(cert.Object, "spec", "isCA")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(isCA).To(BeTrue(), "spec.isCA should be true on the root CA Certificate")
		secretName, _, err := unstructured.NestedString(cert.Object, "spec", "secretName")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(secretName).To(Equal(config.CertName), "spec.secretName should match CertName")
		issuerRefName, _, err := unstructured.NestedString(cert.Object, "spec", "issuerRef", "name")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(issuerRefName).To(Equal(config.IssuerName), "spec.issuerRef.name should match IssuerName")
		issuerRefKind, _, err := unstructured.NestedString(cert.Object, "spec", "issuerRef", "kind")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(issuerRefKind).To(Equal("ClusterIssuer"), "spec.issuerRef.kind should be ClusterIssuer")
		commonName, _, err := unstructured.NestedString(cert.Object, "spec", "commonName")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(commonName).To(Equal(config.CertName), "spec.commonName should match CertName")
		duration, _, err := unstructured.NestedString(cert.Object, "spec", "duration")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(duration).To(Equal("876000h"), "spec.duration should match the configured CA validity period")

		// Assert CA-backed ClusterIssuer was created with the correct secret reference.
		caIssuer, err := getClusterIssuer(ctx, cli, config.CAIssuerName)
		g.Expect(err).NotTo(HaveOccurred())
		caSecretName, _, err := unstructured.NestedString(caIssuer.Object, "spec", "ca", "secretName")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(caSecretName).To(Equal(config.CertName), "spec.ca.secretName should match CertName")
	})

	t.Run("idempotent when applied twice", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(runPipeline()).NotTo(HaveOccurred())

		_, err = getClusterIssuer(ctx, cli, config.IssuerName)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = getRootCACertificate(ctx, cli, config.CertName, config.CertManagerNamespace)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = getClusterIssuer(ctx, cli, config.CAIssuerName)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("recreates externally deleted Certificate", func(t *testing.T) {
		g := NewWithT(t)

		cert := &unstructured.Unstructured{}
		cert.SetGroupVersionKind(gvk.CertManagerCertificate)
		cert.SetName(config.CertName)
		cert.SetNamespace(config.CertManagerNamespace)
		err := cli.Delete(ctx, cert)
		g.Expect(err).NotTo(HaveOccurred())

		_, err = getRootCACertificate(ctx, cli, config.CertName, config.CertManagerNamespace)
		g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "Certificate should be gone after deletion")

		g.Expect(runPipeline()).NotTo(HaveOccurred())

		_, err = getRootCACertificate(ctx, cli, config.CertName, config.CertManagerNamespace)
		g.Expect(err).NotTo(HaveOccurred(), "Certificate should be recreated by the pipeline")
	})

	t.Run("recreates externally deleted self-signed ClusterIssuer", func(t *testing.T) {
		g := NewWithT(t)

		issuer := &unstructured.Unstructured{}
		issuer.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
		issuer.SetName(config.IssuerName)
		err := cli.Delete(ctx, issuer)
		g.Expect(err).NotTo(HaveOccurred())

		_, err = getClusterIssuer(ctx, cli, config.IssuerName)
		g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "self-signed ClusterIssuer should be gone after deletion")

		g.Expect(runPipeline()).NotTo(HaveOccurred())

		_, err = getClusterIssuer(ctx, cli, config.IssuerName)
		g.Expect(err).NotTo(HaveOccurred(), "self-signed ClusterIssuer should be recreated by the pipeline")
	})

	t.Run("recreates externally deleted CA-backed ClusterIssuer", func(t *testing.T) {
		g := NewWithT(t)

		caIssuer := &unstructured.Unstructured{}
		caIssuer.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
		caIssuer.SetName(config.CAIssuerName)
		err := cli.Delete(ctx, caIssuer)
		g.Expect(err).NotTo(HaveOccurred())

		_, err = getClusterIssuer(ctx, cli, config.CAIssuerName)
		g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "CA-backed ClusterIssuer should be gone after deletion")

		g.Expect(runPipeline()).NotTo(HaveOccurred())

		_, err = getClusterIssuer(ctx, cli, config.CAIssuerName)
		g.Expect(err).NotTo(HaveOccurred(), "CA-backed ClusterIssuer should be recreated by the pipeline")
	})

	t.Run("does not create webhook Certificate when Operator is nil", func(t *testing.T) {
		g := NewWithT(t)

		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Controller: controller}
		action, err := certmanager.NewBootstrapAction(config)
		g.Expect(err).NotTo(HaveOccurred())
		err = action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rr.Resources).To(HaveLen(3), "only the 3 PKI resources should be queued")
	})
}

// TestBootstrapWebhookCertificate verifies that NewBootstrapAction creates the operator webhook
// Certificate when Operator is configured.
func TestBootstrapWebhookCertificate(t *testing.T) {
	operatorNamespace := "test-operator-ns-" + xid.New().String()

	config := certmanager.DefaultBootstrapConfig(certmanager.WithOperatorCert(operatorNamespace))

	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	createBootstrapCRDs(t, g, ctx, envTest)
	createBootstrapNamespace(t, g, ctx, cli, config.CertManagerNamespace)
	createBootstrapNamespace(t, g, ctx, cli, operatorNamespace)

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	controller := mocks.NewMockController(func(m *mocks.MockController) {
		m.On("Owns", mock.Anything).Return(false)
	})

	bootstrapAction, err := certmanager.NewBootstrapAction(config)
	g.Expect(err).NotTo(HaveOccurred())
	deployAction := deploy.NewAction(deploy.WithFieldOwner("test-webhook-cert"))

	runPipeline := func() error {
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Controller: controller}
		if err := bootstrapAction(ctx, rr); err != nil {
			return err
		}
		return deployAction(ctx, rr)
	}

	g.Expect(runPipeline()).NotTo(HaveOccurred())

	t.Run("creates webhook Certificate with correct spec", func(t *testing.T) {
		g := NewWithT(t)

		cert, err := getWebhookCertificate(ctx, cli, config.OperatorCertConfig.WebhookCertName, operatorNamespace)
		g.Expect(err).NotTo(HaveOccurred())

		secretName, _, err := unstructured.NestedString(cert.Object, "spec", "secretName")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(secretName).To(Equal(config.OperatorCertConfig.WebhookCertSecretName))

		issuerRefName, _, err := unstructured.NestedString(cert.Object, "spec", "issuerRef", "name")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(issuerRefName).To(Equal(config.CAIssuerName))

		issuerRefKind, _, err := unstructured.NestedString(cert.Object, "spec", "issuerRef", "kind")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(issuerRefKind).To(Equal("ClusterIssuer"))

		dnsNames, _, err := unstructured.NestedStringSlice(cert.Object, "spec", "dnsNames")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(dnsNames).To(ConsistOf(
			fmt.Sprintf("%s.%s.svc", config.OperatorCertConfig.WebhookServiceName, operatorNamespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", config.OperatorCertConfig.WebhookServiceName, operatorNamespace),
		))
	})

	t.Run("idempotent when applied twice", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(runPipeline()).NotTo(HaveOccurred())

		_, err := getWebhookCertificate(ctx, cli, config.OperatorCertConfig.WebhookCertName, operatorNamespace)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("recreates externally deleted webhook Certificate", func(t *testing.T) {
		g := NewWithT(t)

		cert := &unstructured.Unstructured{}
		cert.SetGroupVersionKind(gvk.CertManagerCertificate)
		cert.SetName(config.OperatorCertConfig.WebhookCertName)
		cert.SetNamespace(operatorNamespace)
		err := cli.Delete(ctx, cert)
		g.Expect(err).NotTo(HaveOccurred())

		_, err = getWebhookCertificate(ctx, cli, config.OperatorCertConfig.WebhookCertName, operatorNamespace)
		g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "webhook Certificate should be gone after deletion")

		g.Expect(runPipeline()).NotTo(HaveOccurred())

		_, err = getWebhookCertificate(ctx, cli, config.OperatorCertConfig.WebhookCertName, operatorNamespace)
		g.Expect(err).NotTo(HaveOccurred(), "webhook Certificate should be recreated by the pipeline")
	})

	t.Run("returns error when operator namespace is empty", func(t *testing.T) {
		g := NewWithT(t)

		emptyNSConfig := certmanager.DefaultBootstrapConfig(certmanager.WithOperatorCert(""))

		_, err := certmanager.NewBootstrapAction(emptyNSConfig)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("operator namespace must not be empty"))
	})
}

func TestBootstrapOperatorCertConfig(t *testing.T) {
	tests := []struct {
		name                   string
		namespace              string
		envVars                map[string]string
		expectedNamespace      string
		expectedCertName       string
		expectedCertSecretName string
		expectedServiceName    string
	}{
		{ //nolint:gosec // G101 false positive: field name contains "Secret" but holds a resource name, not a credential
			name:                   "uses the provided namespace",
			namespace:              "my-namespace",
			expectedNamespace:      "my-namespace",
			expectedCertName:       "opendatahub-operator-webhook-cert",
			expectedCertSecretName: "opendatahub-operator-controller-webhook-cert",
			expectedServiceName:    "opendatahub-operator-webhook-service",
		},
		{ //nolint:gosec // G101 false positive: field name contains "Secret" but holds a resource name, not a credential
			name:                   "returns default webhook config when env is unset",
			namespace:              "ns",
			expectedNamespace:      "ns",
			expectedCertName:       "opendatahub-operator-webhook-cert",
			expectedCertSecretName: "opendatahub-operator-controller-webhook-cert",
			expectedServiceName:    "opendatahub-operator-webhook-service",
		},
		{
			name:      "overrides webhook cert secret name from env",
			namespace: "ns",
			envVars: map[string]string{
				certmanager.EnvOperatorWebhookCertSecretName: "custom-secret",
			},
			expectedNamespace:      "ns",
			expectedCertName:       "opendatahub-operator-webhook-cert",
			expectedCertSecretName: "custom-secret",
			expectedServiceName:    "opendatahub-operator-webhook-service",
		},
		{ //nolint:gosec // G101 false positive: field name contains "Secret" but holds a resource name, not a credential
			name:      "overrides webhook service name from env",
			namespace: "ns",
			envVars: map[string]string{
				certmanager.EnvOperatorWebhookServiceName: "custom-service",
			},
			expectedNamespace:      "ns",
			expectedCertName:       "opendatahub-operator-webhook-cert",
			expectedCertSecretName: "opendatahub-operator-controller-webhook-cert",
			expectedServiceName:    "custom-service",
		},
		{ //nolint:gosec // G101 false positive: field name contains "Secret" but holds a resource name, not a credential
			name:      "overrides webhook cert name from env",
			namespace: "ns",
			envVars: map[string]string{
				certmanager.EnvOperatorWebhookCertName: "custom-cert",
			},
			expectedNamespace:      "ns",
			expectedCertName:       "custom-cert",
			expectedCertSecretName: "opendatahub-operator-controller-webhook-cert",
			expectedServiceName:    "opendatahub-operator-webhook-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Clear env vars to prevent test pollution
			t.Setenv(certmanager.EnvOperatorWebhookCertSecretName, "")
			t.Setenv(certmanager.EnvOperatorWebhookServiceName, "")
			t.Setenv(certmanager.EnvOperatorWebhookCertName, "")

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg := certmanager.BootstrapOperatorCertConfig(tt.namespace)
			g.Expect(cfg.Namespace).To(Equal(tt.expectedNamespace))
			g.Expect(cfg.WebhookCertName).To(Equal(tt.expectedCertName))
			g.Expect(cfg.WebhookCertSecretName).To(Equal(tt.expectedCertSecretName))
			g.Expect(cfg.WebhookServiceName).To(Equal(tt.expectedServiceName))
		})
	}
}

func TestWithOperatorCert(t *testing.T) {
	t.Run("sets OperatorCertConfig on BootstrapConfig", func(t *testing.T) {
		g := NewWithT(t)

		config := certmanager.DefaultBootstrapConfig(certmanager.WithOperatorCert("test-ns"))
		g.Expect(config.OperatorCertConfig).NotTo(BeNil())
		g.Expect(config.OperatorCertConfig.Namespace).To(Equal("test-ns"))
	})

	t.Run("OperatorCertConfig is nil without WithOperatorCert", func(t *testing.T) {
		g := NewWithT(t)

		config := certmanager.DefaultBootstrapConfig()
		g.Expect(config.OperatorCertConfig).To(BeNil())
	})
}
