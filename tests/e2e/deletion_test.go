package e2e_test

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type DeletionTestCtx struct {
	*TestContext
}

// deletionTestSuite runs the deletion test suite.
func deletionTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	deletionTestCtx := DeletionTestCtx{
		TestContext: tc,
	}

	// Define the test cases
	testCases := []TestCase{
		{"Delete DataScienceCluster instance", deletionTestCtx.TestDSCDeletion},
		{"Delete DSCInitialization instance", deletionTestCtx.TestDSCIDeletion},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// TestDSCDeletion verifies that DSCI-managed monitoring survives DSC deletion.
func (tc *DeletionTestCtx) TestDSCDeletion(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)

	skipUnless(t, Tier3)

	t.Log("Enable DSCI-managed monitoring")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(jq.Match(
			`any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeReady, metav1.ConditionTrue,
		)),
		WithEventuallyTimeout(15*time.Minute),
	)
	dsci := tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
	)
	dsc := tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
	)
	dsciUID := string(dsci.GetUID())
	dscUID := string(dsc.GetUID())
	platform := tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
	)
	platformUID := string(platform.GetUID())

	// Reconcile DSC after DSCI and verify the shared Platform retains both
	// non-controller owner references.
	t.Log("Reconcile DSC after DSCI to verify both Platform owners are retained")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(
			`(.metadata.annotations //= {}) | .metadata.annotations."test.opendatahub.io/force-reconcile" = "%d"`,
			time.Now().UnixNano(),
		)),
	)
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithCondition(And(
			jq.Match(`.metadata.uid == "%s"`, platformUID),
			jq.Match(`any(.metadata.ownerReferences[]; .kind == "%s" and .name == "%s" and .uid == "%s")`, gvk.DSCInitialization.Kind, tc.DSCInitializationNamespacedName.Name, dsciUID),
			jq.Match(`any(.metadata.ownerReferences[]; .kind == "%s" and .name == "%s" and .uid == "%s")`, gvk.DataScienceCluster.Kind, tc.DataScienceClusterNamespacedName.Name, dscUID),
		)),
		WithConsistentlyDuration(30*time.Second),
		WithConsistentlyPollingInterval(5*time.Second),
	)

	t.Log("Delete DataScienceCluster and verify Platform/Monitoring remain healthy")
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithWaitForDeletion(true),
	)
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(jq.Match(`.metadata.deletionTimestamp == null and .metadata.uid == "%s"`, dsciUID)),
		WithConsistentlyDuration(30*time.Second),
		WithConsistentlyPollingInterval(5*time.Second),
	)
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithCondition(And(
			jq.Match(`.metadata.deletionTimestamp == null`),
			jq.Match(`.metadata.uid == "%s"`, platformUID),
			jq.Match(`any(.status.conditions[]; .type == "Ready" and .status == "True")`),
			jq.Match(`any(.metadata.ownerReferences[]; .kind == "%s" and .name == "%s" and .uid == "%s")`, gvk.DSCInitialization.Kind, tc.DSCInitializationNamespacedName.Name, dsciUID),
		)),
		WithConsistentlyDuration(30*time.Second),
		WithConsistentlyPollingInterval(5*time.Second),
	)
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(jq.Match(
			`.metadata.deletionTimestamp == null and any(.status.conditions[]; .type == "%s" and .status == "%s")`,
			status.ConditionTypeReady, metav1.ConditionTrue,
		)),
		WithConsistentlyDuration(30*time.Second),
		WithConsistentlyPollingInterval(5*time.Second),
	)
}

// TestDSCIDeletion deletes the DSCInitialization instance if it exists.
func (tc *DeletionTestCtx) TestDSCIDeletion(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)

	skipUnless(t, Tier3)

	// Delete the DSCInitialization instance
	tc.DeleteResource(WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName))
}
