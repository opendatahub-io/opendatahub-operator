package cloudmanager_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega/format"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

const rhaiOperatorNamespace = "opendatahub-operator-system"

var (
	tc         *testf.TestContext
	provider   ProviderConfig
	chartsPath string
	pki        pkiNames
)

func TestMain(m *testing.M) {
	format.MaxLength = 0 // 0 disables truncation entirely
	format.RegisterCustomFormatter(stripManagedFields)

	providerName := os.Getenv("CLOUD_MANAGER_PROVIDER")
	if providerName == "" {
		fmt.Println("CLOUD_MANAGER_PROVIDER not set, skipping cloud manager e2e tests")
		os.Exit(0)
	}

	p, ok := providers[providerName]
	if !ok {
		valid := make([]string, 0, len(providers))
		for _, v := range providers {
			valid = append(valid, v.Name)
		}
		fmt.Fprintf(os.Stderr, "unknown CLOUD_MANAGER_PROVIDER %q (valid: %v)\n", providerName, strings.Join(valid, ", "))
		os.Exit(1)
	}
	provider = p

	rootPath, err := envtestutil.FindProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find project root: %v\n", err)
		os.Exit(1)
	}
	chartsPath = filepath.Join(rootPath, "opt", "charts")

	tc, err = testf.NewTestContext(
		testf.WithTOptions(
			testf.WithEventuallyTimeout(5*time.Minute),
			testf.WithEventuallyPollingInterval(2*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create test context: %v\n", err)
		os.Exit(1)
	}

	pki, err = discoverPKIConfig(tc, provider.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to discover PKI config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("PKI config: issuer=%s, cert=%s, caIssuer=%s, ns=%s\n",
		pki.IssuerName, pki.CertName, pki.CAIssuerName, pki.CertManagerNamespace)

	if err := preflightCheck(); err != nil {
		fmt.Fprintf(os.Stderr, "preflight check failed: %v\n", err)
		os.Exit(1)
	}

	// precreate operator ns (would be created by helm chart, but we are installing directly)
	operatorNs := &unstructured.Unstructured{}
	operatorNs.SetGroupVersionKind(gvk.Namespace)
	operatorNs.SetName(rhaiOperatorNamespace)
	if err := tc.Client().Create(tc.Context(), operatorNs); err != nil && !k8serr.IsAlreadyExists(err) {
		fmt.Fprintf(os.Stderr, "failed to create operator namespace: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// preflightCheck verifies that the cloud manager CRD is installed on the cluster.
// This distinguishes "cloud manager not deployed" from actual test failures.
func preflightCheck() error {
	crdName := strings.ToLower(provider.GVK.Kind) + "s." + provider.GVK.Group

	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(gvk.CustomResourceDefinition)

	err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: crdName}, crd)
	if err != nil {
		return fmt.Errorf(
			"CRD %q not found — is the cloud manager deployed?\n\n"+
				"  The e2e tests expect the cloud manager operator to be running on the cluster.\n"+
				"  Deploy it first, then re-run the tests.\n\n"+
				"  Error: %w", crdName, err,
		)
	}

	// Fail if a leftover CR exists from a previous run.
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(provider.GVK)

	nn := types.NamespacedName{Name: provider.InstanceName}
	err = tc.Client().Get(tc.Context(), nn, existing)

	switch {
	case k8serr.IsNotFound(err):
		// clean slate
	case err != nil:
		return fmt.Errorf("failed to check for leftover CR %q: %w", provider.InstanceName, err)
	default:
		return fmt.Errorf("leftover %s %q exists — delete it before running the tests", provider.GVK.Kind, provider.InstanceName)
	}

	return nil
}

func stripManagedFields(value any) (string, bool) {
	switch v := value.(type) {
	case *unstructured.Unstructured:
		c := v.DeepCopy()
		c.SetManagedFields(nil)
		redactSecretData(c)

		return format.Object(c.Object, 1), true
	case unstructured.Unstructured:
		c := v.DeepCopy()
		c.SetManagedFields(nil)
		redactSecretData(c)

		return format.Object(c.Object, 1), true
	case []unstructured.Unstructured:
		stripped := make([]map[string]any, 0, len(v))
		for i := range v {
			c := v[i].DeepCopy()
			c.SetManagedFields(nil)
			redactSecretData(c)
			stripped = append(stripped, c.Object)
		}

		return format.Object(stripped, 1), true
	default:
		return "", false
	}
}

func redactSecretData(obj *unstructured.Unstructured) {
	if obj.GetKind() != "Secret" {
		return
	}

	for _, field := range []string{"data", "stringData"} {
		if m, ok := obj.Object[field].(map[string]any); ok {
			for k := range m {
				m[k] = "<redacted>"
			}
		}
	}
}
