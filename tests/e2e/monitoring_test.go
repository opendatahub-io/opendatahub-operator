package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
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

	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed)),
	)

	// Define test cases.
	testCases := []TestCase{
		{"Auto creation of Monitoring CR", monitoringServiceCtx.ValidateMonitoringCRCreation},
		{"Test Monitoring CR content default value", monitoringServiceCtx.ValidateMonitoringCRDefaultContent},
		{"Test Metrics MonitoringStack CR Creation", monitoringServiceCtx.ValidateMonitoringStackCRMetricsWhenSet},
		{"Test Metrics MonitoringStack CR Configuration", monitoringServiceCtx.ValidateMonitoringStackCRMetricsConfiguration},
		{"Test Metrics Replicas Configuration", monitoringServiceCtx.ValidateMonitoringStackCRMetricsReplicasUpdate},
		{"Test MonitoringStack CR Deletion", monitoringServiceCtx.ValidateMonitoringStackCRDeleted},
		{"Test Monitoring CR Deletion", monitoringServiceCtx.ValidateMonitoringCRDeleted},
		{"Test Traces default content", monitoringServiceCtx.ValidateMonitoringCRDefaultTracesContent},
		{"Test TempoMonolithic CR Creation with PV backend", monitoringServiceCtx.ValidateTempoMonolithicCRCreation},
		{"Test TempoMonolithic CR Configuration", monitoringServiceCtx.ValidateTempoMonolithicCRConfiguration},
		{"Test TempoStack CR Creation with S3 backend", monitoringServiceCtx.ValidateTempoStackCRCreation},
		{"Test TempoStack CR Configuration", monitoringServiceCtx.ValidateTempoStackCRConfiguration},
		{"Test TempoStack CR Creation with GCS backend", monitoringServiceCtx.ValidateTempoStackCRCreationWithGCS},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateMonitoringCRCreation ensures that exactly one Monitoring CR exists and status to Ready.
func (tc *MonitoringTestCtx) ValidateMonitoringCRCreation(t *testing.T) {
	t.Helper()

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(
			And(
				HaveLen(1),
				HaveEach(And(
					jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DSCInitialization.Kind),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
				)),
			),
		),
	)
}

// ValidateMonitoringCRDefaultContent validates when no "metrics" is set in DSCI.
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

	// Validate metrics is nil when not set in DSCI
	tc.g.Expect(monitoring.Spec.Metrics).
		To(BeNil(), "Expected metrics to be nil when not set in DSCI")

	// Validate MontoringStack CR is not created
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsWhenSet(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Update DSCI to set metrics - ensure managementState remains Managed
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringMetrics(),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.spec.metrics != null`)),
		WithCustomErrorMsg("Monitoring resource should be updated with metrics configuration by DSCInitialization controller"),
	)

	// ensure the MonitoringStack CR is created (status conditions are set by external monitoring operator)
	ms := tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeAvailable, metav1.ConditionTrue)),
	)
	tc.g.Expect(ms).ToNot(BeNil())
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Use EnsureResourceExists with jq matchers for cleaner validation
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(And(
			// Validate storage size is set to 5Gi
			jq.Match(`.spec.prometheusConfig.persistentVolumeClaim.resources.requests.storage == "%s"`, "5Gi"),
			// Validate storage retention is set to 1d
			jq.Match(`.spec.retention == "%s"`, "1d"),
			// Validate CPU request is set to 250m
			jq.Match(`.spec.resources.requests.cpu == "%s"`, "250m"),
			// Validate memory request is set to 350Mi
			jq.Match(`.spec.resources.requests.memory == "%s"`, "350Mi"),
			// Validate CPU limit defaults to 500m
			jq.Match(`.spec.resources.limits.cpu == "%s"`, "500m"),
			// Validate memory limit defaults to 512Mi
			jq.Match(`.spec.resources.limits.memory == "%s"`, "512Mi"),
			// Validate replicas is set to 2 when it was not specified in DSCI
			jq.Match(`.spec.prometheusConfig.replicas == %d`, 2),
			// Validate owner references
			jq.Match(`.metadata.ownerReferences | length == 1`),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Monitoring.Kind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, "default-monitoring"),
		)),
		WithCustomErrorMsg("MonitoringStack '%s' configuration validation failed", "data-science-monitoringstack"),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsReplicasUpdate(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Update DSCI to set replicas to 1 (must include either storage or resources due to CEL validation rule)
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.metrics = %s`, `{storage: {size: "5Gi", retention: "1d"}, replicas: 1}`)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(And(
			// Validate storage size is still the same value
			jq.Match(`.spec.prometheusConfig.persistentVolumeClaim.resources.requests.storage == "%s"`, "5Gi"),
			// Validate replicas is set to 1 when it is updated in DSCI
			jq.Match(`.spec.prometheusConfig.replicas == %d`, 1),
		)),
		WithCustomErrorMsg("MonitoringStack '%s' configuration validation failed", "data-science-monitoringstack"),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRDeleted(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Verify MonitoringStack CR is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
	)

	// Set metrics to empty object
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.metrics = %s`, `{}`)),
	)

	// Verify MonitoringStack CR is deleted by gc
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
	)

	// Ensure Monitoring CR is still present
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
	)

	monitoring := &serviceApi.Monitoring{}
	tc.FetchTypedResource(monitoring, WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))
	tc.g.Expect(monitoring.Spec.Metrics).To(BeNil(), "Expected 'metrics' to be nil in Monitoring CR")
}

func (tc *MonitoringTestCtx) ValidateMonitoringCRDeleted(t *testing.T) {
	t.Helper()

	// Set Monitroing to be removed
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, "Removed")),
	)

	// Ensure Monitoring CR is removed because of ownerreference
	tc.EnsureResourcesGone(WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))
}

func getTempoMonolithicName(dsci *dsciv1.DSCInitialization) string {
	return "data-science-tempomonolithic"
}

func getTempoStackName(dsci *dsciv1.DSCInitialization) string {
	return "data-science-tempostack"
}

// setMonitoringMetrics creates a transformation function that sets the monitoring metrics configuration.
func setMonitoringMetrics() testf.TransformFn {
	return func(obj *unstructured.Unstructured) error {
		metricsConfig := map[string]interface{}{
			"storage": map[string]interface{}{
				"size":      "5Gi",
				"retention": "1d",
			},
			"resources": map[string]interface{}{
				"cpurequest":    "250m",
				"memoryrequest": "350Mi",
			},
		}

		return unstructured.SetNestedField(obj.Object, metricsConfig, "spec", "monitoring", "metrics")
	}
}

// setMonitoringTraces creates a transformation function that sets the monitoring traces configuration.
func setMonitoringTraces(backend, secret, size string) testf.TransformFn {
	return func(obj *unstructured.Unstructured) error {
		tracesConfig := map[string]interface{}{
			"storage": map[string]interface{}{
				"backend": backend,
			},
		}

		if size != "" {
			if storage, ok := tracesConfig["storage"].(map[string]interface{}); ok {
				storage["size"] = size
			}
		}

		if secret != "" {
			if storage, ok := tracesConfig["storage"].(map[string]interface{}); ok {
				storage["secret"] = secret
			}
		}

		return unstructured.SetNestedField(obj.Object, tracesConfig, "spec", "monitoring", "traces")
	}
}

// ValidateMonitoringCRDefaultTracesContent validates that traces stanza is omitted by default.
func (tc *MonitoringTestCtx) ValidateMonitoringCRDefaultTracesContent(t *testing.T) {
	t.Helper()

	// Ensure monitoring is enabled (might have been disabled by previous test)
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed)),
	)

	// Wait for the Monitoring resource to be created/updated by DSCInitialization controller
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
		WithCustomErrorMsg("Monitoring resource should be created by DSCInitialization controller"),
	)

	// Ensure that the Monitoring resource exists.
	monitoring := &serviceApi.Monitoring{}
	tc.FetchTypedResource(monitoring, WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))

	// Validate the traces stanza is omitted by default
	tc.g.Expect(monitoring.Spec.Traces).
		To(BeNil(),
			"Expected traces stanza to be omitted by default")
}

// ValidateTempoMonolithicCRCreation tests creation of TempoMonolithic CR with PV backend.
func (tc *MonitoringTestCtx) ValidateTempoMonolithicCRCreation(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoMonolithicName := getTempoMonolithicName(dsci)

	// Update DSCI to set traces with PV backend
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces("pv", "", "10Gi"),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
		WithCondition(jq.Match(`.spec.traces != null`)),
		WithCustomErrorMsg("Monitoring resource should be updated with traces configuration by DSCInitialization controller"),
	)

	// Ensure the TempoMonolithic CR is created (status conditions are set by external tempo operator).
	tempoMonolithic := tc.EnsureResourceExists(
		WithMinimalObject(gvk.TempoMonolithic, types.NamespacedName{Name: tempoMonolithicName, Namespace: dsci.Spec.Monitoring.Namespace}),
	)
	tc.g.Expect(tempoMonolithic).ToNot(BeNil())
}

// ValidateTempoMonolithicCRConfiguration tests configuration of TempoMonolithic CR.
func (tc *MonitoringTestCtx) ValidateTempoMonolithicCRConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoMonolithicName := getTempoMonolithicName(dsci)

	tempoMonolithic := tc.FetchResources(
		WithMinimalObject(gvk.TempoMonolithic, types.NamespacedName{Name: tempoMonolithicName, Namespace: dsci.Spec.Monitoring.Namespace}),
	)

	// Validate the storage size is set to 10Gi.
	storageSize, found, err := unstructured.NestedString(tempoMonolithic[0].Object, "spec", "storage", "traces", "size")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(storageSize).To(Equal("10Gi"))

	// Validate the backend is set to pv.
	backend, found, err := unstructured.NestedString(tempoMonolithic[0].Object, "spec", "storage", "traces", "backend")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(backend).To(Equal("pv"))
}

// ValidateTempoStackCRCreation tests creation of TempoStack CR with S3 backend.
func (tc *MonitoringTestCtx) ValidateTempoStackCRCreation(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoStackName := getTempoStackName(dsci)

	// Update DSCI to set traces with S3 backend.
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces("s3", "s3-secret", ""),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.spec.traces != null`)),
		WithCustomErrorMsg("Monitoring resource should be updated with traces configuration by DSCInitialization controller"),
	)

	// Ensure the TempoStack CR is created (status conditions are set by external tempo operator).
	tempoStack := tc.EnsureResourceExists(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{Name: tempoStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
	)
	tc.g.Expect(tempoStack).ToNot(BeNil())
}

// ValidateTempoStackCRConfiguration tests configuration of TempoStack CR.
func (tc *MonitoringTestCtx) ValidateTempoStackCRConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoStackName := getTempoStackName(dsci)

	tempoStack := tc.FetchResources(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{Name: tempoStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
	)

	// Validate the backend is set to s3.
	backend, found, err := unstructured.NestedString(tempoStack[0].Object, "spec", "storage", "secret", "type")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(backend).To(Equal("s3"))

	// Validate the secret name is set.
	secretName, found, err := unstructured.NestedString(tempoStack[0].Object, "spec", "storage", "secret", "name")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(secretName).To(Equal("s3-secret"))
}

// ValidateTempoStackCRCreationWithGCS tests creation of TempoStack CR with GCS backend.
func (tc *MonitoringTestCtx) ValidateTempoStackCRCreationWithGCS(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoStackName := getTempoStackName(dsci)

	// Update DSCI to set traces with GCS backend.
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces("gcs", "gcs-secret", ""),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.spec.traces.storage.backend == "gcs"`)),
		WithCustomErrorMsg("Monitoring resource should be updated with GCS traces configuration by DSCInitialization controller"),
	)

	// Wait for the TempoStack CR to be updated with the GCS backend (this ensures the resource is updated after the previous S3 test).
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{Name: tempoStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.spec.storage.secret.type == "gcs"`)),
		WithCustomErrorMsg("TempoStack resource should be updated with GCS backend type"),
	)

	// Fetch the updated TempoStack to validate its configuration
	tempoStack := tc.EnsureResourceExists(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{Name: tempoStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
	)
	tc.g.Expect(tempoStack).ToNot(BeNil())

	// Validate the backend is set to gcs.
	backend, found, err := unstructured.NestedString(tempoStack.Object, "spec", "storage", "secret", "type")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(backend).To(Equal("gcs"))

	// Validate the secret name is set.
	secretName, found, err := unstructured.NestedString(tempoStack.Object, "spec", "storage", "secret", "name")
	tc.g.Expect(err).ToNot(HaveOccurred())
	tc.g.Expect(found).To(BeTrue())
	tc.g.Expect(secretName).To(Equal("gcs-secret"))
}
