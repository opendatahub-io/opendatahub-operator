package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
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

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed)),
	)

	// Define test cases.
	testCases := []TestCase{
		{"Auto creation of Monitoring CR", monitoringServiceCtx.ValidateMonitoringCRCreation},
		{"Test Monitoring CR content", monitoringServiceCtx.ValidateMonitoringCRDefaultContent},
		{"Test Metrics Defaults", monitoringServiceCtx.ValidateMonitoringCrMetricsDefaults},
		{"Test Metrics MonitoringStack CR Creation", monitoringServiceCtx.ValidateMonitoringCrMetricsWhenSet},
		{"Test Metrics MonitoringStack CR Configuration", monitoringServiceCtx.ValidateMonitoringCrMetricsConfiguration},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateMonitoringCRCreation ensures that exactly one Monitoring CR exists.
func (tc *MonitoringTestCtx) ValidateMonitoringCRCreation(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))
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
		MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{Namespace: "opendatahub", Metrics: nil},
	}
	// Validate the metrics stanza is omitted by default
	tc.g.Expect(monitoring.Spec.Metrics).
		To(Equal(comp.Metrics),
			"Expected metrics stanza to be omitted by default")
}

func (tc *MonitoringTestCtx) ValidateMonitoringCrMetricsWhenSet(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	monitoringStackName := getMonitoringStackName(dsci)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.metrics = %s`, `{storage: {size: 5, retention: 1}, resources: {cpurequest: "250", memoryrequest: "350"}}`)),
	)

	ms := tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: monitoringStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, "Available", "True")),
	)

	tc.g.Expect(ms).ToNot(BeNil())
}

func (tc *MonitoringTestCtx) ValidateMonitoringCrMetricsConfiguration(t *testing.T) {
	t.Helper()

	monitoring := &serviceApi.Monitoring{}
	tc.FetchTypedResource(monitoring, WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))

	dsci := tc.FetchDSCInitialization()
	monitoringStackName := getMonitoringStackName(dsci)

	ms := tc.FetchResources(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: monitoringStackName}),
	)

	// Validate the storage size is set to 5Gi
	storageSize, found, err := unstructured.NestedString(ms[0].Object, "spec", "prometheusConfig", "persistentVolumeClaim", "resources", "requests", "storage")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(storageSize).To(Equal("5Gi"))

	// Validate the resources are set to the correct values
	cpuRequest, found, err := unstructured.NestedString(ms[0].Object, "spec", "resources", "requests", "cpu")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(cpuRequest).To(Equal("250m"))

	memoryRequest, found, err := unstructured.NestedString(ms[0].Object, "spec", "resources", "requests", "memory")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(memoryRequest).To(Equal("350Mi"))

	// Validate the resources defaults are set to the correct values
	cpuLimit, found, err := unstructured.NestedString(ms[0].Object, "spec", "resources", "limits", "cpu")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(cpuLimit).To(Equal("500m"))

	memoryLimit, found, err := unstructured.NestedString(ms[0].Object, "spec", "resources", "limits", "memory")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(memoryLimit).To(Equal("512Mi"))
}

func getMonitoringStackName(dsci *dsciv1.DSCInitialization) string {
	switch dsci.Status.Release.Name {
	case cluster.ManagedRhoai:
		return "rhoai-monitoringstack"
	case cluster.SelfManagedRhoai:
		return "rhoai-monitoringstack"
	case cluster.OpenDataHub:
		return "odh-monitoringstack"
	}

	return "odh-monitoringstack"
}
