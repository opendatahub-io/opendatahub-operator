package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

type WorkbenchesTestCtx struct {
	*ComponentTestCtx
}

func workbenchesTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Workbenches{})
	require.NoError(t, err)

	componentCtx := WorkbenchesTestCtx{
		ComponentTestCtx: ct,
	}

	require.NoError(t, err)

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
		{"Validate resource quota exceeded", componentCtx.ValidateResourceQuotaExceeded},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateResourceQuotaExceeded tests how operator handles resource quota violations.
func (tc *WorkbenchesTestCtx) ValidateResourceQuotaExceeded(t *testing.T) {
	t.Helper()

	t.Log("Testing resource quota exceeded - should report quota-related deployment issues")

	t.Log("Fetching original DSC")
	originalDSC := tc.FetchDataScienceCluster()

	t.Log("Getting current workbench namespace")
	currentWorkbenchNS := originalDSC.Spec.Components.Workbenches.WorkbenchNamespace
	if currentWorkbenchNS == "" {
		// default value
		currentWorkbenchNS = "opendatahub"
	}

	// Create restrictive quota in the existing workbench namespace
	t.Log("Creating restrictive quota")
	restrictiveQuota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-restrictive-quota",
			Namespace: currentWorkbenchNS,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
				corev1.ResourcePods:   resource.MustParse("1"),
			},
		},
	}

	// Ensure we restore state and clean up after the test
	defer func() {
		t.Log("Restoring DSC")
		tc.EnsureResourceCreatedOrUpdated(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.TransformPipeline(
				testf.Transform(`.spec.components.workbenches.managementState = "%s"`, originalDSC.Spec.Components.Workbenches.ManagementState),
			)),
		)
		t.Log("Cleaning up test quota")
		tc.DeleteResource(WithObjectToCreate(restrictiveQuota))
	}()

	t.Log("Applying restrictive quota to existing workbench namespace")
	tc.EnsureResourceCreatedOrUpdated(WithObjectToCreate(restrictiveQuota))

	t.Log("Enabling workbenches (will use existing namespace with restrictive quota)")
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.workbenches.managementState = "Managed"`)),
	)

	t.Log("Checking that underlying notebook controller deployment shows the actual quota error")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Name: "odh-notebook-controller-manager", Namespace: currentWorkbenchNS}),
		WithCondition(jq.Match(`(.status.conditions[] | select(.type == "ReplicaFailure" and .status == "True" and (.message | contains("exceeded quota")))) != null`)),
	)

	t.Log("Checking that DataScienceCluster reports the workbenches issue")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`(.status.conditions[] | select(.type == "ComponentsReady" and .status == "False" and (.message | contains("Some components are not ready: workbenches")))) != null`)),
	)

	t.Log("Resource quota exceeded correctly reported in conditions")
}
