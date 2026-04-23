package cloudmanager_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
)

func TestMain(m *testing.M) {
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
