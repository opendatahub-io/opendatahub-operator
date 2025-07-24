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

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/monitoring"
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

	tc.EventuallyResourceCreatedOrUpdated(
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
		{"Test Traces default content", monitoringServiceCtx.ValidateMonitoringCRDefaultTracesContent},
		{"Test TempoMonolithic CR Creation with PV backend", monitoringServiceCtx.ValidateTempoMonolithicCRCreation},
		{"Test TempoStack CR Creation with S3 backend", monitoringServiceCtx.ValidateTempoStackCRCreationWithS3},
		{"Test TempoStack CR Creation with GCS backend", monitoringServiceCtx.ValidateTempoStackCRCreationWithGCS},
		{"Test OpenTelemetry Collector Deployment", monitoringServiceCtx.ValidateOpenTelemetryCollectorDeployment},
		{"Test OpenTelemetry Collector Traces Configuration", monitoringServiceCtx.ValidateOpenTelemetryCollectorTracesConfiguration},
		{"Test Instrumentation CR Traces Creation", monitoringServiceCtx.ValidateInstrumentationCRTracesWhenSet},
		{"Test Instrumentation CR Traces Configuration", monitoringServiceCtx.ValidateInstrumentationCRTracesConfiguration},
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
	tc.EventuallyResourceCreatedOrUpdated(
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
	tc.EventuallyResourceCreatedOrUpdated(
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

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.spec.metrics == null`)),
		WithCustomErrorMsg("Monitoring CR should have null metrics"),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringCRDeleted(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Set Monitoring to be removed
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.managementState = "%s"`, "Removed")),
	)

	// Ensure Monitoring CR is removed because of ownerreference
	tc.EnsureResourcesGone(WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{Name: "data-science-collector", Namespace: dsci.Spec.Monitoring.Namespace}),
		// Format of statusReplicas is n/m, we check if at least one is ready
		WithCondition(jq.Match(`.status.scale.statusReplicas | split("/") | min > 0`)),
	)
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
		WithMutateFunc(testf.Transform(`.spec.monitoring.traces = %s`, `{storage: {backend: "pv"}}`)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{Name: "data-science-collector", Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.spec.config.service.pipelines | has("traces")`)),
	)
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

// ValidateTempoMonolithicCRCreation tests creation of TempoMonolithic CR with PV backend.
func (tc *MonitoringTestCtx) ValidateTempoMonolithicCRCreation(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tempoMonolithicName := getTempoMonolithicName(dsci)

	// Update DSCI to set traces with PV backend
	tc.EventuallyResourceCreatedOrUpdated(
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
			),
		),
		WithCustomErrorMsg("TempoMonolithic CR should be created when traces are configured"),
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
			setMonitoringTraces("pv", "", ""),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(jq.Match(`.spec.traces != null`)),
		WithCustomErrorMsg("Monitoring resource should be updated with traces configuration by DSCInitialization controller"),
	)

	// Ensure the Instrumentation CR is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: monitoring.InstrumentationName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCustomErrorMsg("Instrumentation CR should be created when traces are configured"),
	)
}

// ValidateInstrumentationCRTracesConfiguration validates the content of the Instrumentation CR with Traces.
func (tc *MonitoringTestCtx) ValidateInstrumentationCRTracesConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Wait for the Instrumentation CR to be created and stabilized by the OpenTelemetry operator
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: monitoring.InstrumentationName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(And(
			jq.Match(`.spec != null`),
			jq.Match(`.metadata.generation >= 1`),
		)),
		WithCustomErrorMsg("Instrumentation CR should be created and have a valid spec"),
	)

	// Fetch the Instrumentation CR and validate its content with Eventually for stability
	expectedEndpoint := fmt.Sprintf("http://data-science-collector.%s.svc.cluster.local:4317", dsci.Spec.Monitoring.Namespace)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: monitoring.InstrumentationName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(
			And(
				// Validate the exporter endpoint is set correctly
				jq.Match(`.spec.exporter.endpoint == "%s"`, expectedEndpoint),
				// Validate the sampler configuration
				jq.Match(`.spec.sampler.type == "%s"`, monitoring.DefaultSamplerType),
				jq.Match(`.spec.sampler.argument == "0.1"`),
			),
		),
		WithCustomErrorMsg("Instrumentation CR should have the expected configuration"),
	)

	// Validate owner references
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: monitoring.InstrumentationName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(
			And(
				jq.Match(`.metadata.ownerReferences | length == 1`),
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Monitoring.Kind),
				jq.Match(`.metadata.ownerReferences[0].name == "%s"`, "default-monitoring"),
			),
		),
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
	tempoStackName := getTempoStackName(dsci)

	// Update DSCI to set traces with specified backend
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			setMonitoringTraces(backend, secretName, ""),
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
				// Validate that the Tempo operator has accepted and reconciled the resource
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			),
		),
		WithCustomErrorMsg(
			"TempoStack should be created with %s backend, but was not found or has incorrect backend type",
			backend,
		),
	)

	// Making sure it get deleted at the end of the test
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
