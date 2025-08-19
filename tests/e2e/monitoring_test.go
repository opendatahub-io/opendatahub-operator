package e2e_test

import (
	"fmt"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

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

	// Reset monitoring to default state to ensure clean test environment
	// This handles cases where previous tests (like restrictive quota test) modified monitoring config
	dsci := tc.FetchDSCInitialization()
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(
			`.spec.monitoring = {managementState: "Managed", namespace: "%s"}`,
			dsci.Spec.Monitoring.Namespace,
		)),
	)

	// Define test cases.
	testCases := []TestCase{
		{"Auto creation of Monitoring CR", monitoringServiceCtx.ValidateMonitoringCRCreation},
		{"Test Monitoring CR content default value", monitoringServiceCtx.ValidateMonitoringCRDefaultContent},
		{"Test Traces default content", monitoringServiceCtx.ValidateMonitoringCRDefaultTracesContent},
		{"Test Metrics MonitoringStack CR Creation", monitoringServiceCtx.ValidateMonitoringStackCRMetricsWhenSet},
		{"Test Metrics MonitoringStack CR Configuration", monitoringServiceCtx.ValidateMonitoringStackCRMetricsConfiguration},
		{"Test Metrics Replicas Configuration", monitoringServiceCtx.ValidateMonitoringStackCRMetricsReplicasUpdate},
		{"Test Prometheus Rule Creation", monitoringServiceCtx.ValidatePrometheusRuleCreation},
		{"Test Prometheus Rule Deletion", monitoringServiceCtx.ValidatePrometheusRuleDeletion},
		{"Test TempoMonolithic CR Creation with PV backend", monitoringServiceCtx.ValidateTempoMonolithicCRCreation},
		{"Test TempoStack CR Creation with S3 backend", monitoringServiceCtx.ValidateTempoStackCRCreationWithS3},
		{"Test TempoStack CR Creation with GCS backend", monitoringServiceCtx.ValidateTempoStackCRCreationWithGCS},
		{"Test OpenTelemetry Collector Deployment", monitoringServiceCtx.ValidateOpenTelemetryCollectorDeployment},
		{"Test OpenTelemetry Collector Traces Configuration", monitoringServiceCtx.ValidateOpenTelemetryCollectorTracesConfiguration},
		{"Test OpenTelemetry Collector Custom Metrics Exporters", monitoringServiceCtx.ValidateOpenTelemetryCollectorCustomMetricsExporters},
		{"Test Instrumentation CR Traces Creation", monitoringServiceCtx.ValidateInstrumentationCRTracesWhenSet},
		{"Test Instrumentation CR Traces Configuration", monitoringServiceCtx.ValidateInstrumentationCRTracesConfiguration},
		{"Test OpenTelemetry Collector Custom Traces Exporters", monitoringServiceCtx.ValidateOpenTelemetryCollectorCustomTracesExporters},
		{"Test Traces Exporters Reserved Name Validation", monitoringServiceCtx.ValidateTracesExportersReservedNameValidation},
		{"Test MonitoringStack CR Deletion", monitoringServiceCtx.ValidateMonitoringStackCRDeleted},
		{"Test Monitoring CR Deletion", monitoringServiceCtx.ValidateMonitoringCRDeleted},
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
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(
			And(
				jq.Match(`.spec.namespace == "%s"`, dsci.Spec.Monitoring.Namespace),
				jq.Match(`.spec.metrics == null`),
			),
		),
		WithCustomErrorMsg("Monitoring CR should have expected namespace and null metrics"),
	)

	// Validate MontoringStack CR is not created
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsWhenSet(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Update DSCI to set metrics - ensure managementState remains Managed
	tc.EventuallyResourceCreatedOrUpdated(
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
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeAvailable, metav1.ConditionTrue)),
	)
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
			// Validate storage retention is set to 90d
			jq.Match(`.spec.retention == "%s"`, "90d"),
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
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.metrics = %s`, `{storage: {size: "5Gi", retention: "90d"}, replicas: 1}`)),
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
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, "Managed"),
			testf.Transform(`.spec.monitoring.metrics = null`),
			testf.Transform(`.spec.monitoring.traces = null`),
			testf.Transform(`.spec.monitoring.namespace = "%s"`, dsci.Spec.Monitoring.Namespace),
		)),
	)

	// Verify MonitoringStack CR is deleted by gc
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: "data-science-monitoringstack", Namespace: dsci.Spec.Monitoring.Namespace}),
	)

	// Ensure Monitoring CR is still present
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.spec.metrics == null`)),
		WithCustomErrorMsg("Monitoring CR should have null metrics"),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringCRDeleted(t *testing.T) {
	t.Helper()
	// Set Monitoring to be removed
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, "Removed")),
	)

	// Ensure Monitoring CR is removed because of ownerreference
	tc.EnsureResourcesGone(WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))
}

func (tc *MonitoringTestCtx) ValidateOpenTelemetryCollectorDeployment(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{Name: "data-science-collector", Namespace: dsci.Spec.Monitoring.Namespace}),
		// Format of statusReplicas is n/m, we check if at least one is ready
		WithCondition(jq.Match(`.status.scale.statusReplicas | split("/") | min > 0`)),
	)
}

func (tc *MonitoringTestCtx) ValidateOpenTelemetryCollectorTracesConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces("pv", "", "", "720h"), // Use 30 days retention for this test
		)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{Name: "data-science-collector", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.spec.config.service.pipelines | has("traces")`)),
	)

	// Cleanup: Reset DSCInitialization traces configuration to prevent state contamination
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = null`)),
	)
}

func (tc *MonitoringTestCtx) ValidateOpenTelemetryCollectorCustomTracesExporters(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Set traces configuration with custom exporters
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = %s`, `{
			storage: {backend: "pv"},
			exporters: {
				"debug": {
					verbosity: "detailed"
				},
				"otlphttp/custom": {
					endpoint: "http://custom-endpoint:4318",
					headers: {
						"api-key": "secret-key"
					}
				}
			}
		}`)),
	)

	// Validate that the OpenTelemetry collector has the custom exporters configured
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{Name: "data-science-collector", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.spec.config.exporters | has("debug")`)),
		WithCondition(jq.Match(`.spec.config.exporters | has("otlphttp/custom")`)),
		WithCondition(jq.Match(`.spec.config.exporters | has("otlp/tempo")`)), // Default tempo exporter should still exist
		WithCondition(jq.Match(`.spec.config.service.pipelines.traces.exporters | contains(["debug"])`)),
		WithCondition(jq.Match(`.spec.config.service.pipelines.traces.exporters | contains(["otlphttp/custom"])`)),
		WithCondition(jq.Match(`.spec.config.service.pipelines.traces.exporters | contains(["otlp/tempo"])`)),
	)

	// Cleanup: Reset DSCInitialization traces configuration to prevent state contamination
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = null`)),
	)
}

func (tc *MonitoringTestCtx) ValidateTracesExportersReservedNameValidation(t *testing.T) {
	t.Helper()

	// Attempt to set traces configuration with a reserved exporter name
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = %s`, `{
			storage: {backend: "pv"},
			exporters: {
				"otlp/tempo": {
					endpoint: "http://malicious-endpoint:4317"
				}
			}
		}`)),
	)

	// Validate that the Monitoring resource reports an error condition due to reserved name
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse)),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("reserved")`, status.ConditionTypeProvisioningSucceeded)),
	)

	// Cleanup: Reset DSCInitialization traces configuration to clear the error state
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = null`)),
	)
}

func getTempoMonolithicName() string {
	return "data-science-tempomonolithic"
}

func getTempoStackName() string {
	return "data-science-tempostack"
}

// setMonitoringMetrics creates a transformation function that sets the monitoring metrics configuration.
func setMonitoringMetrics() testf.TransformFn {
	return func(obj *unstructured.Unstructured) error {
		metricsConfig := map[string]interface{}{
			"storage": map[string]interface{}{
				"size":      "5Gi",
				"retention": "90d",
			},
			"resources": map[string]interface{}{
				"cpurequest":    "250m",
				"memoryrequest": "350Mi",
			},
			"replicas": int64(2),
		}

		return unstructured.SetNestedField(obj.Object, metricsConfig, "spec", "monitoring", "metrics")
	}
}

// setMonitoringTraces creates a transformation function that sets the monitoring traces configuration.
func setMonitoringTraces(backend, secret, size, retention string) testf.TransformFn {
	return func(obj *unstructured.Unstructured) error {
		tracesConfig := map[string]interface{}{
			"storage": map[string]interface{}{
				"backend": backend,
			},
			"exporters": map[string]interface{}{},
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

		if retention != "" {
			if storage, ok := tracesConfig["storage"].(map[string]interface{}); ok {
				storage["retention"] = retention
			}
		}

		return unstructured.SetNestedField(obj.Object, tracesConfig, "spec", "monitoring", "traces")
	}
}

// ValidateMonitoringCRDefaultTracesContent validates that traces stanza is omitted by default.
func (tc *MonitoringTestCtx) ValidateMonitoringCRDefaultTracesContent(t *testing.T) {
	t.Helper()

	// Ensure monitoring is enabled (might have been disabled by previous test)
	tc.EventuallyResourceCreatedOrUpdated(
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
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.spec.traces == null`)),
		WithCustomErrorMsg("Expected traces stanza to be omitted by default"),
	)
}

// ValidateTempoMonolithicCRCreation tests creation of TempoMonolithic CR with PV backend and custom retention.
func (tc *MonitoringTestCtx) ValidateTempoMonolithicCRCreation(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoMonolithicName := getTempoMonolithicName()

	// Update DSCI to set traces with PV backend
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces("pv", "", "10Gi", "24h"),
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
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.TempoMonolithic, types.NamespacedName{Name: tempoMonolithicName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(
			And(
				// Validate it's ready
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
				// Validate the storage size is set to 10Gi
				jq.Match(`.spec.storage.traces.size == "10Gi"`),
				// Validate the backend is set to pv
				jq.Match(`.spec.storage.traces.backend == "pv"`),
				// Validate retention is set to 24h
				jq.Match(`.spec.extraConfig.tempo.compactor.compaction.block_retention == "24h0m0s"`),
			),
		),
		WithCustomErrorMsg("TempoMonolithic CR should be created when traces are configured"),
	)

	// Cleanup: Reset DSCInitialization traces configuration and delete TempoMonolithic
	// This ensures proper test isolation and prevents state contamination between tests
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = null`)),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.TempoMonolithic, types.NamespacedName{Name: tempoMonolithicName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithWaitForDeletion(true),
	)
}

// ValidateTempoStackCRCreationWithS3 tests creation of TempoStack CR with S3 backend.
func (tc *MonitoringTestCtx) ValidateTempoStackCRCreationWithS3(t *testing.T) {
	t.Helper()

	tc.validateTempoStackCreationWithBackend(
		t,
		"s3",
		"s3-secret",
		jq.Match(`.spec.traces != null`),
		"Monitoring resource should be updated with traces configuration by DSCInitialization controller",
	)
}

// ValidateTempoStackCRCreationWithGCS tests creation of TempoStack CR with GCS backend.
func (tc *MonitoringTestCtx) ValidateTempoStackCRCreationWithGCS(t *testing.T) {
	t.Helper()

	// First, perform the basic TempoStack creation validation
	tc.validateTempoStackCreationWithBackend(t,
		"gcs",
		"gcs-secret",
		jq.Match(`.spec.traces.storage.backend == "gcs"`),
		"Monitoring resource should be updated with GCS traces configuration by DSCInitialization controller")
}

// ValidateInstrumentationCRTracesWhenSet validates the content of the Instrumentation CR.
func (tc *MonitoringTestCtx) ValidateInstrumentationCRTracesWhenSet(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Update DSCI to set traces - ensure managementState remains Managed
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces("pv", "", "", ""),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(
			And(
				jq.Match(`.spec.traces != null`),
				jq.Match(`.spec.traces.storage.retention == "2160h0m0s"`),
			),
		),
		WithCustomErrorMsg("Monitoring resource should be updated with traces configuration by DSCInitialization controller"),
	)

	// Ensure the Instrumentation CR is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: "data-science-instrumentation", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCustomErrorMsg("Instrumentation CR should be created when traces are configured"),
	)

	// Cleanup: Reset DSCInitialization traces configuration to prevent state contamination
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = null`)),
	)
}

// ValidateInstrumentationCRTracesConfiguration validates the content of the Instrumentation CR with Traces.
func (tc *MonitoringTestCtx) ValidateInstrumentationCRTracesConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Wait for the Instrumentation CR to be created and stabilized by the OpenTelemetry operator
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: "data-science-instrumentation", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(And(
			jq.Match(`.spec != null`),
			jq.Match(`.metadata.generation >= 1`),
		)),
		WithCustomErrorMsg("Instrumentation CR should be created and have a valid spec"),
	)

	// Fetch the Instrumentation CR and validate its content with Eventually for stability
	expectedEndpoint := fmt.Sprintf("http://data-science-collector.%s.svc.cluster.local:4317", dsci.Spec.Monitoring.Namespace)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: "data-science-instrumentation", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(
			And(
				// Validate the exporter endpoint is set correctly
				jq.Match(`.spec.exporter.endpoint == "%s"`, expectedEndpoint),
				// Validate the sampler configuration
				jq.Match(`.spec.sampler.type == "%s"`, "traceidratio"),
				jq.Match(`.spec.sampler.argument == "0.1"`),
			),
		),
		WithCustomErrorMsg("Instrumentation CR should have the expected configuration"),
	)

	// Validate owner references
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: "data-science-instrumentation", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(
			And(
				jq.Match(`.metadata.ownerReferences | length == 1`),
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Monitoring.Kind),
				jq.Match(`.metadata.ownerReferences[0].name == "%s"`, "default-monitoring"),
			),
		),
	)

	// Cleanup: Reset DSCInitialization traces configuration to prevent state contamination
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = null`)),
	)
}

// validateTempoStackCreationWithBackend validates TempoStack creation with the specified backend.
// It updates the DSCInitialization to configure monitoring traces, waits for the Monitoring resource
// to be updated, and ensures the TempoStack CR is created with the correct backend configuration.
//
// Parameters:
//   - backend: The storage backend type (e.g., "s3", "gcs")
//   - secretName: The name of the secret containing backend credentials
//   - monitoringCondition: Gomega matcher to validate the Monitoring resource state
//   - monitoringErrorMsg: Error message to display if Monitoring resource validation fails
//
// Returns the created TempoStack resource for additional validation by the caller.
func (tc *MonitoringTestCtx) validateTempoStackCreationWithBackend(
	t *testing.T,
	backend, secretName string,
	monitoringCondition gTypes.GomegaMatcher,
	monitoringErrorMsg string,
) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoStackName := getTempoStackName()

	// Update DSCI to set traces with specified backend
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces(backend, secretName, "", "100m"),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(monitoringCondition),
		WithCustomErrorMsg(monitoringErrorMsg),
	)

	// Create dummy secret
	tc.createDummySecret(backend, secretName, dsci.Spec.Monitoring.Namespace)

	// Ensure the TempoStack CR is created with specified backend
	// (status conditions are set by external tempo operator)
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{
			Name:      tempoStackName,
			Namespace: dsci.Spec.Monitoring.Namespace,
		}),
		WithCondition(
			And(
				// Validate the backend is set correctly
				jq.Match(`.spec.storage.secret.type == "%s"`, backend),
				jq.Match(`.spec.storage.secret.name == "%s"`, secretName),
				// Validate retention is set correctly
				jq.Match(`.spec.retention.global.traces == "%s"`, "1h40m0s"), // to match 100m
				// Validate that the Tempo operator has accepted and reconciled the resource
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			),
		),
		WithCustomErrorMsg(
			"TempoStack should be created with %s backend, but was not found or has incorrect backend type",
			backend,
		),
	)

	// Cleanup: Reset DSCInitialization traces configuration, delete TempoStack and secret
	// This ensures proper test isolation and prevents state contamination between tests
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = null`)),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{Name: tempoStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithWaitForDeletion(true),
	)
	tc.DeleteResource(
		WithMinimalObject(gvk.Secret, types.NamespacedName{Name: secretName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithWaitForDeletion(true),
	)
}

// createDummySecret creates a dummy secret for TempoStack testing (S3 or GCS).
func (tc *MonitoringTestCtx) createDummySecret(backendType, secretName, namespace string) {
	var secret *corev1.Secret

	switch backendType {
	case "s3":
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"access_key_id":     []byte("fake-access-key"),
				"access_key_secret": []byte("fake-secret-key"),
				"bucket":            []byte("fake-bucket"),
				"endpoint":          []byte("https://s3.amazonaws.com"),
				// No region field - causes TempoStack validation conflicts
			},
		}
	case "gcs":
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"key.json": []byte(`{
					"type": "service_account",
					"project_id": "fake-test-project-not-real",
					"private_key_id": "test-key-id-fake",
					"private_key": "-----BEGIN PRIVATE KEY-----\nTEST-FAKE-KEY-NOT-REAL\n-----END PRIVATE KEY-----\n",
					"client_email": "test-fake@fake-project.iam.gserviceaccount.com"
                }`),
			},
		}
	default:
		tc.g.Fail(fmt.Sprintf("Unsupported backend type: %s", backendType))
		return
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(secret),
	)
}

func (tc *MonitoringTestCtx) ValidatePrometheusRuleCreation(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Update DSCI to enable alerting (requires metrics to be configured per validation rule)
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringMetrics(),
			testf.Transform(`.spec.monitoring.alerting = {}`),
		)),
	)

	// Update DSC to enable dashboard component
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, types.NamespacedName{Name: "e2e-test-dsc", Namespace: tc.DSCInitializationNamespacedName.Namespace}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.dashboard.managementState = "%s"`, operatorv1.Managed),
		)),
	)

	// Ensure the dashboard resource exists and is marked "Ready".
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Dashboard, types.NamespacedName{Name: "default-dashboard", Namespace: dsci.Spec.ApplicationsNamespace}),
		WithCondition(
			And(
				HaveLen(1),
				HaveEach(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
				)),
			),
		),
	)

	// Ensure the prometheus rules exist
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.PrometheusRule, types.NamespacedName{Name: "dashboard-prometheusrules", Namespace: dsci.Spec.ApplicationsNamespace}),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.PrometheusRule, types.NamespacedName{Name: "operator-prometheusrules", Namespace: dsci.Spec.ApplicationsNamespace}),
	)
}

func (tc *MonitoringTestCtx) ValidatePrometheusRuleDeletion(t *testing.T) {
	t.Helper()

	// Update DSC to disable dashboard component
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, types.NamespacedName{Name: "e2e-test-dsc", Namespace: tc.DSCInitializationNamespacedName.Namespace}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.dashboard.managementState = "%s"`, operatorv1.Removed),
		)),
	)

	// Ensure the dashboard-prometheusrules is deleted
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.PrometheusRule, types.NamespacedName{Name: "dashboard-prometheusrules", Namespace: tc.AppsNamespace}),
	)

	// Cleanup: Remove alerting configuration from DSCInitialization to prevent validation issues
	// This ensures that subsequent tests can set metrics=null without violating the validation rule
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.alerting = null`)),
	)
}

func (tc *MonitoringTestCtx) ValidateOpenTelemetryCollectorCustomMetricsExporters(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Configure DSCI with custom metrics exporters
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringMetricsWithCustomExporters(),
		)),
	)

	// First verify the Monitoring service is ready
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`)),
	)

	// Verify OpenTelemetry Collector has custom exporters configured
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{Name: "data-science-collector", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(And(
			jq.Match(`.spec.config.exporters | has("prometheus")`),  // Built-in exporter
			jq.Match(`.spec.config.exporters | has("logging")`),     // Custom exporter 1
			jq.Match(`.spec.config.exporters | has("otlp/custom")`), // Custom exporter 2
			jq.Match(`.spec.config.exporters.logging.loglevel == "debug"`),
			jq.Match(`.spec.config.exporters."otlp/custom".endpoint == "http://custom-backend:4317"`),
			jq.Match(`.spec.config.exporters."otlp/custom".tls.insecure == true`),
			jq.Match(`.spec.config.service.pipelines.metrics.exporters | length == 3`), // prometheus + 2 custom
			jq.Match(`.spec.config.service.pipelines.metrics.exporters | contains(["prometheus"])`),
			jq.Match(`.spec.config.service.pipelines.metrics.exporters | contains(["logging"])`),
			jq.Match(`.spec.config.service.pipelines.metrics.exporters | contains(["otlp/custom"])`),
		)),
	)
}

// setMonitoringMetricsWithCustomExporters creates a transformation function that sets monitoring metrics with custom exporters.
func setMonitoringMetricsWithCustomExporters() testf.TransformFn {
	return func(obj *unstructured.Unstructured) error {
		metricsConfig := map[string]interface{}{
			"storage": map[string]interface{}{
				"size":      "5Gi",
				"retention": "90d",
			},
			"resources": map[string]interface{}{
				"cpurequest":    "250m",
				"memoryrequest": "350Mi",
			},
			"exporters": map[string]interface{}{
				"logging":     "loglevel: debug",
				"otlp/custom": "endpoint: http://custom-backend:4317\ntls:\n  insecure: true",
			},
		}

		return unstructured.SetNestedField(obj.Object, metricsConfig, "spec", "monitoring", "metrics")
	}
}
