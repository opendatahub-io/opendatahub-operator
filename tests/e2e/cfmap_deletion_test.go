package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"

	. "github.com/onsi/gomega"
)

// CfgMapDeletionTestCtx holds the context for the config map deletion tests.
type CfgMapDeletionTestCtx struct {
	*TestContext
	ConfigMapNamespacedName types.NamespacedName
}

// cfgMapDeletionTestSuite runs the testing flow for DataScienceCluster deletion logic via ConfigMap.
func cfgMapDeletionTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	cfgMapDeletionTestCtx := &CfgMapDeletionTestCtx{
		TestContext: tc,
		ConfigMapNamespacedName: types.NamespacedName{
			Name:      "delete-configmap-name",
			Namespace: tc.OperatorNamespace,
		},
	}

	// Ensure ConfigMap cleanup after tests
	defer cfgMapDeletionTestCtx.RemoveDeletionConfigMap(t)

	// Define test cases
	testCases := []TestCase{
		{name: "Validate creation of configmap with deletion disabled", testFn: cfgMapDeletionTestCtx.ValidateDSCDeletionUsingConfigMap},
		{name: "Validate that owned namespaces are not deleted", testFn: cfgMapDeletionTestCtx.ValidateOwnedNamespacesAllExist},
	}

	// Run the test suite
	RunTestCases(t, testCases)
}

// ValidateDSCDeletionUsingConfigMap tests the deletion of DataScienceCluster based on the config map setting.
func (tc *CfgMapDeletionTestCtx) ValidateDSCDeletionUsingConfigMap(t *testing.T) {
	t.Helper()

	// Create or update the deletion config map
	enableDeletion := "false"
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      tc.ConfigMapNamespacedName.Name,
			Namespace: tc.ConfigMapNamespacedName.Namespace,
			Labels: map[string]string{
				upgrade.DeleteConfigMapLabel: enableDeletion,
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(configMap),
		WithCustomErrorMsg("Failed to create or update deletion config map"),
	)

	// Verify the existence of the DataScienceCluster instance.
	tc.EnsureResourceExists(WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName))
}

// ValidateOwnedNamespacesAllExist verifies that the owned namespaces exist.
func (tc *CfgMapDeletionTestCtx) ValidateOwnedNamespacesAllExist(t *testing.T) {
	t.Helper()

	// Ensure namespaces with the owned namespace label exist
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.SelectorFromSet(
					k8slabels.Set{labels.ODH.OwnedNamespace: "true"},
				),
			}),
		WithCondition(BeNumerically(">=", ownedNamespaceNumber)),
		WithCustomErrorMsg("Expected only %%d owned namespaces with label '%s'. Owned namespaces should not be deleted.", ownedNamespaceNumber, labels.ODH.OwnedNamespace),
	)
}

// RemoveDeletionConfigMap ensures the deletion of the ConfigMap.
func (tc *CfgMapDeletionTestCtx) RemoveDeletionConfigMap(t *testing.T) {
	t.Helper()

	// Delete the config map
	propagationPolicy := metav1.DeletePropagationForeground
	tc.DeleteResource(
		WithMinimalObject(gvk.ConfigMap, tc.ConfigMapNamespacedName),
		WithClientDeleteOptions(
			&client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			}),
	)
}
