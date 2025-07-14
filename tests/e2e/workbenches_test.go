package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
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

	originalDSC := tc.FetchDataScienceCluster()

	workbenchNS := getWorkbenchNamespace(originalDSC)

	t.Log("Creating restrictive quota in workbench namespace")
	restrictiveQuota := createRestrictiveQuota(workbenchNS)
	tc.EnsureResourceCreatedOrUpdated(WithObjectToCreate(restrictiveQuota))

	t.Log("Enabling workbenches in the DataScienceCluster")
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.workbenches.managementState = "Managed"`)),
	)

	deploymentName := "odh-notebook-controller-manager"

	t.Log("Checking that underlying notebook controller deployment shows the actual quota error")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name: deploymentName, Namespace: workbenchNS,
		}),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "ReplicaFailure" and .status == "True") | .message | contains("exceeded quota")`),
		),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	t.Log("Checking that DataScienceCluster reports the workbenches issue")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "ComponentsReady" and .status == "False") | .message | test("some components are not ready: workbenches"; "i")`),
		),
		WithCustomErrorMsg("DSC should report workbenches component issues due to quota constraints"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	t.Log("Restoring original workbenches state and cleaning up quota")

	t.Log("Removing restrictive quota")
	tc.DeleteResource(WithObjectToCreate(restrictiveQuota))

	t.Log("Restoring workbenches state")
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.workbenches.managementState = "%s"`,
			originalDSC.Spec.Components.Workbenches.ManagementState)),
	)

	t.Log("Resource quota validation completed successfully")
}

func getWorkbenchNamespace(dsc *dscv1.DataScienceCluster) string {
	if ns := dsc.Spec.Components.Workbenches.WorkbenchNamespace; ns != "" {
		return ns
	}
	return "opendatahub"
}

func createRestrictiveQuota(namespace string) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ResourceQuota",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-restrictive-quota",
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("10m"),
				corev1.ResourceRequestsMemory: resource.MustParse("10Mi"),
				corev1.ResourceLimitsCPU:      resource.MustParse("20m"),
				corev1.ResourceLimitsMemory:   resource.MustParse("20Mi"),
				corev1.ResourcePods:           resource.MustParse("1"),
			},
		},
	}
}
