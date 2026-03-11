package cloudmanager_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

var (
	tc       *testf.TestContext
	provider ProviderConfig
)

func TestMain(m *testing.M) {
	providerName := os.Getenv("CLOUD_MANAGER_PROVIDER")
	if providerName == "" {
		fmt.Println("CLOUD_MANAGER_PROVIDER not set, skipping cloud manager e2e tests")
		os.Exit(0)
	}

	p, ok := providers[providerName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown CLOUD_MANAGER_PROVIDER %q (valid: azure, coreweave)\n", providerName)
		os.Exit(1)
	}
	provider = p

	var err error
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
	operatorNs.SetName("opendatahub-operator-system")
	_ = tc.Client().Create(tc.Context(), operatorNs)

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

	// Delete any leftover CR from a previous test run.
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(provider.GVK)

	nn := types.NamespacedName{Name: provider.InstanceName}
	if err := tc.Client().Get(tc.Context(), nn, existing); err == nil {
		fmt.Printf("preflight: deleting leftover %s %q\n", provider.GVK.Kind, provider.InstanceName)

		if err := tc.Client().Delete(tc.Context(), existing); err != nil {
			return fmt.Errorf("failed to delete leftover CR: %w", err)
		}
	}

	return nil
}
