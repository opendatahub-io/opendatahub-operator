package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type MonitoringTestCtx struct {
	*TestContext
}

func monitoringTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	monitoringServiceCtx := MonitoringTestCtx{
		TestContext: tc,
	}

	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed)),
	)

	// Define test cases.
	testCases := []TestCase{
		{"Auto creation of Monitoring CR", monitoringServiceCtx.ValidateMonitoringCRCreation},
		{"Test Monitoring CR content", monitoringServiceCtx.ValidateMonitoringCRDefaultContent},
		{"Test Metrics Defaults", monitoringServiceCtx.ValidateMonitoringCrMetricsDefaults},
		{"Test Metrics MonitoringStack CR Creation", monitoringServiceCtx.ValidateMonitoringCrMetricsWhenSet},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateMonitoringCRCreation ensures that exactly one Monitoring CR exists.
func (tc *MonitoringTestCtx) ValidateMonitoringCRCreation(t *testing.T) {
	t.Helper()

	// Retrieve the DSCInitialization object.
	dsci := tc.FetchDSCInitialization()
	// If this isn't a managed cluster the monitoring CR won't be created by default
	if dsci.Status.Release.Name != cluster.ManagedRhoai {
		mon := getMonitoringObject(tc, false)

		tc.EnsureResourceCreatedOrUpdated(
			WithObjectToCreate(&mon),
		)
	}

	// Ensure exactly one Monitoring CR exists.
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{}),
		WithCondition(HaveLen(1)),
		WithCustomErrorMsg(
			"Expected exactly one resource of kind '%s', but found a different number of resources.",
			gvk.Monitoring.Kind,
		),
	)
}

// ValidateMonitoringCRDefaultContent validates the default content of the Monitoring CR.
func (tc *MonitoringTestCtx) ValidateMonitoringCRDefaultContent(t *testing.T) {
	t.Helper()

	// Retrieve the DSCInitialization object.
	dsci := tc.FetchDSCInitialization()

	// Ensure that the Monitoring resource exists.
	monitoring := &serviceApi.Monitoring{}
	tc.FetchTypedResource(monitoring, WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))

	// Validate that the Monitoring CR's namespace matches the DSCInitialization spec.
	tc.g.Expect(monitoring.Spec.MonitoringCommonSpec.Namespace).
		To(Equal(dsci.Spec.Monitoring.Namespace),
			"Monitoring CR's namespace mismatch: Expected namespace '%v' as per DSCInitialization, but found '%v' in Monitoring CR.",
			dsci.Spec.Monitoring.Namespace, monitoring.Spec.Namespace)
}

func (tc *MonitoringTestCtx) ValidateMonitoringCrMetricsDefaults(t *testing.T) {
	t.Helper()

	monitoring := &serviceApi.Monitoring{}
	tc.FetchTypedResource(monitoring, WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))

	comp := serviceApi.MonitoringSpec{
		MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{Namespace: "opendatahub"},
		Metrics:              nil,
	}
	// Validate the metrics stanza is omitted by default
	tc.g.Expect(monitoring.Spec.Metrics).
		To(Equal(comp.Metrics),
			"Expected metrics stanza to be omitted by default")
}

func (tc *MonitoringTestCtx) ValidateMonitoringCrMetricsWhenSet(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	mon := getMonitoringObject(tc, true)

	tc.DeleteResource(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithWaitForDeletion(true),
	)

	tc.EnsureResourceCreatedOrUpdated(
		WithObjectToCreate(&mon),
	)

	var monitoringStackName string
	switch dsci.Status.Release.Name {
	case cluster.ManagedRhoai:
		monitoringStackName = "rhoai-monitoringstack"
	case cluster.SelfManagedRhoai:
		monitoringStackName = "rhoai-monitoringstack"
	case cluster.OpenDataHub:
		monitoringStackName = "odh-monitoringstack"
	default:
		monitoringStackName = "odh-monitoringstack"
	}

	ms := tc.FetchResources(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: monitoringStackName}),
	)

	tc.g.Expect(len(ms)).
		To(Equal(1),
			"Expected exactly one resource of kind '%s', but found '%d'.", gvk.MonitoringStack, len(ms))
}

func getMonitoringObject(tc *MonitoringTestCtx, metrics bool) serviceApi.Monitoring {
	dsci := tc.FetchDSCInitialization()

	base := serviceApi.Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-monitoring",
		},
		Spec: serviceApi.MonitoringSpec{
			MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
				Namespace: dsci.Spec.Monitoring.Namespace,
			},
		},
		Status: serviceApi.MonitoringStatus{},
	}

	enabled := serviceApi.Metrics{
		Storage: serviceApi.MetricsStorage{
			Size: 5,
		},
	}

	if metrics {
		base.Spec.Metrics = &enabled
	}

	return base
}
