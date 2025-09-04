package e2e_test

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// Constants for monitoring resource names.
const (
	MonitoringCRName           = "default-monitoring"
	MonitoringStackName        = "data-science-monitoringstack"
	OpenTelemetryCollectorName = "data-science-collector"
	TempoMonolithicName        = "data-science-tempomonolithic"
	TempoStackName             = "data-science-tempostack"
	InstrumentationName        = "data-science-instrumentation"
)

// Constants for common test values.
const (
	DefaultRetention       = "100m"
	FormattedRetention     = "1h40m0s" // 100m in TempoStack format
	MetricsStorageSize     = "5Gi"
	MetricsRetention       = "90d"
	OtlpCustomExporter     = "otlp/custom"
	OtlpHttpCustomExporter = "otlphttp/custom"
	OtlpTempoExporter      = "otlp/tempo"
	MetricsCPURequest      = "100m"
	MetricsMemoryRequest   = "256Mi"
	MetricsCPULimit        = "500m"
	MetricsMemoryLimit     = "512Mi"
	MetricsDefaultReplicas = 2

	// TracesStorage backend types for testing.
	TracesStorageBackendPV        = "pv"
	TracesStorageBackendS3        = "s3"
	TracesStorageBackendGCS       = "gcs"
	TracesStorageBackendS3Secret  = "s3-secret"
	TracesStorageBackendGCSSecret = "gcs-secret"
	TracesStorageRetention        = "720h"
	TracesStorageRetention24h     = "24h"
	TracesStorageSize10Gi         = "10Gi"
)

// monitoringOwnerReferencesCondition is a reusable condition for validating owner references.
var monitoringOwnerReferencesCondition = And(
	jq.Match(`.metadata.ownerReferences | length == 1`),
	jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Monitoring.Kind),
	jq.Match(`.metadata.ownerReferences[0].name == "%s"`, MonitoringCRName),
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

	// Define test cases.
	testCases := []TestCase{
		{"Auto creation of Monitoring CR", monitoringServiceCtx.ValidateMonitoringCRCreation},
		{"Test Monitoring CR content default value", monitoringServiceCtx.ValidateMonitoringCRDefaultContent},
		{"Test Traces default content", monitoringServiceCtx.ValidateMonitoringCRDefaultTracesContent},
		{"Test Metrics MonitoringStack CR Creation", monitoringServiceCtx.ValidateMonitoringStackCRMetricsWhenSet},
		{"Test Metrics MonitoringStack CR Configuration", monitoringServiceCtx.ValidateMonitoringStackCRMetricsConfiguration},
		{"Test Metrics Replicas Configuration", monitoringServiceCtx.ValidateMonitoringStackCRMetricsReplicasUpdate},
		{"Test Prometheus rules lifecycle", monitoringServiceCtx.ValidatePrometheusRulesLifecycle},
		{"Test TempoMonolithic CR Creation with PV backend", monitoringServiceCtx.ValidateTempoMonolithicCRCreation},
		{"Test TempoStack CR Creation with Cloud Storage", monitoringServiceCtx.ValidateTempoStackCRCreationWithCloudStorage},
		{"Test OpenTelemetry Collector Configurations", monitoringServiceCtx.ValidateOpenTelemetryCollectorConfigurations},
		{"Test OpenTelemetry Collector replicas", monitoringServiceCtx.ValidateMonitoringCRCollectorReplicas},
		{"Test Instrumentation CR Traces Creation", monitoringServiceCtx.ValidateInstrumentationCRTracesWhenSet},
		{"Test Instrumentation CR Traces Configuration", monitoringServiceCtx.ValidateInstrumentationCRTracesConfiguration},
		// {"Test Traces Exporters Reserved Name Validation", monitoringServiceCtx.ValidateTracesExportersReservedNameValidation},
		{"Validate CEL blocks invalid monitoring configs", monitoringServiceCtx.ValidateCELBlocksInvalidMonitoringConfigs},
		{"Validate CEL allows valid monitoring configs", monitoringServiceCtx.ValidateCELAllowsValidMonitoringConfigs},
		{"Validate monitoring service disabled", monitoringServiceCtx.ValidateMonitoringServiceDisabled},
		{"Test Namespace Restricted Metrics Access", monitoringServiceCtx.ValidatePrometheusRestrictedResourceConfiguration},
		{"Test Prometheus Secure Proxy Authentication", monitoringServiceCtx.ValidatePrometheusSecureProxyAuthentication},
		{"Test Metrics API Authentication and Authorization", monitoringServiceCtx.ValidateMetricsAPIAuthenticationAndAuthorization},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateMonitoringCRCreation ensures that exactly one Monitoring CR exists and status to Ready.
func (tc *MonitoringTestCtx) ValidateMonitoringCRCreation(t *testing.T) {
	t.Helper()

	tc.updateMonitoringConfig(withManagementState(operatorv1.Managed))

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
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

	// Ensure that the Monitoring resource exists.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(
			And(
				jq.Match(`.spec.namespace == "%s"`, tc.MonitoringNamespace),
				jq.Match(`.spec.metrics == null`),
			),
		),
		WithCustomErrorMsg("Monitoring CR should have expected namespace and null metrics"),
	)

	// Validate MontoringStack CR is not created
	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: MonitoringStackName, Namespace: tc.MonitoringNamespace}),
	)
}

// ValidateMonitoringStackCRMetricsWhenSet validates that MonitoringStack CR is created with correct metrics configuration when metrics are set in DSCI.
func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsWhenSet(t *testing.T) {
	t.Helper()

	// Update DSCI to set metrics - ensure managementState remains Managed
	tc.updateMonitoringConfig(
		withManagementState(operatorv1.Managed),
		withMetricsConfig(),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(jq.Match(`.spec.metrics != null`)),
		WithCustomErrorMsg("Monitoring resource should be updated with metrics configuration by DSCInitialization controller"),
	)

	// ensure the MonitoringStack CR is created (status conditions are set by external monitoring operator)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: MonitoringStackName, Namespace: tc.MonitoringNamespace}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeAvailable, metav1.ConditionTrue)),
	)
}

// ValidateMonitoringStackCRMetricsConfiguration verifies that MonitoringStack CR contains the correct metrics storage size, retention, and resource limits.
func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsConfiguration(t *testing.T) {
	t.Helper()

	// Use EnsureResourceExists with jq matchers for cleaner validation
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: MonitoringStackName, Namespace: tc.MonitoringNamespace}),
		WithCondition(And(
			// Validate storage size is set to MetricsStorageSize
			jq.Match(`.spec.prometheusConfig.persistentVolumeClaim.resources.requests.storage == "%s"`, MetricsStorageSize),
			// Validate storage retention is set to MetricsRetention
			jq.Match(`.spec.retention == "%s"`, MetricsRetention),
			// Validate CPU request is set to MetricsCPURequest
			jq.Match(`.spec.resources.requests.cpu == "%s"`, MetricsCPURequest),
			// Validate memory request is set to MetricsMemoryRequest
			jq.Match(`.spec.resources.requests.memory == "%s"`, MetricsMemoryRequest),
			// Validate CPU limit defaults to MetricsCPULimit
			jq.Match(`.spec.resources.limits.cpu == "%s"`, MetricsCPULimit),
			// Validate memory limit defaults to MetricsMemoryLimit
			jq.Match(`.spec.resources.limits.memory == "%s"`, MetricsMemoryLimit),
			// Validate replicas is set to MetricsDefaultReplicas when it was not specified in DSCI
			jq.Match(`.spec.prometheusConfig.replicas == %d`, MetricsDefaultReplicas),
			// Validate owner references
			monitoringOwnerReferencesCondition,
		)),
		WithCustomErrorMsg("MonitoringStack '%s' configuration validation failed", MonitoringStackName),
	)
}

// ValidateMonitoringStackCRMetricsReplicasUpdate tests that MonitoringStack CR replicas are updated correctly when metrics replicas are changed.
func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsReplicasUpdate(t *testing.T) {
	t.Helper()

	// Update DSCI to set replicas to 1 (must include either storage or resources due to CEL validation rule)
	replicasTransforms := append(
		[]testf.TransformFn{
			testf.Transform(`.spec.monitoring.metrics.storage.size = "%s"`, MetricsStorageSize),
			testf.Transform(`.spec.monitoring.metrics.storage.retention = "%s"`, MetricsRetention),
		},
		withMetricsReplicas(1),
	)
	tc.updateMonitoringConfig(replicasTransforms...)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: MonitoringStackName, Namespace: tc.MonitoringNamespace}),
		WithCondition(And(
			// Validate storage size is still the same value
			jq.Match(`.spec.prometheusConfig.persistentVolumeClaim.resources.requests.storage == "%s"`, MetricsStorageSize),
			// Validate replicas is set to 1 when it is updated in DSCI
			jq.Match(`.spec.prometheusConfig.replicas == %d`, 1),
		)),
		WithCustomErrorMsg("MonitoringStack '%s' configuration validation failed", MonitoringStackName),
	)
}

// ValidateCELBlocksInvalidMonitoringConfigs tests that CEL validation blocks invalid monitoring configurations.
func (tc *MonitoringTestCtx) ValidateCELBlocksInvalidMonitoringConfigs(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		transforms  testf.TransformFn
		description string
	}{
		{
			name: "alerting_with_empty_metrics",
			transforms: testf.TransformPipeline(
				withManagementState(operatorv1.Managed),
				withEmptyMetrics(),
				withEmptyAlerting(),
			),
			description: "Empty metrics object should block alerting configuration",
		},
		{
			name: "alerting_without_metrics_field",
			transforms: testf.TransformPipeline(
				withManagementState(operatorv1.Managed),
				withNoMetrics(),
				withEmptyAlerting(),
			),
			description: "Missing metrics field should trigger XValidation error",
		},
		{
			name: "alerting_with_only_exporters",
			transforms: testf.TransformPipeline(
				withManagementState(operatorv1.Managed),
				testf.Transform(`.spec.monitoring.metrics = {"exporters": {"custom": "config"}}`),
				withEmptyAlerting(),
			),
			description: "Exporters alone should not satisfy alerting requirements",
		},
		{
			name: "replicas_without_storage_or_resources",
			transforms: testf.TransformPipeline(
				withManagementState(operatorv1.Managed),
				testf.Transform(`.spec.monitoring.metrics = {"replicas": 2}`),
			),
			description: "Non-zero replicas should require storage or resources",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.EventuallyResourceCreatedOrUpdated(
				WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
				WithMutateFunc(testCase.transforms),
				WithAcceptableErr(k8serr.IsInvalid, "IsInvalid"),
			)
		})
	}
}

// ValidateCELAllowsValidMonitoringConfigs tests that CEL validation allows valid monitoring configurations.
func (tc *MonitoringTestCtx) ValidateCELAllowsValidMonitoringConfigs(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		transforms  testf.TransformFn
		description string
	}{
		{
			name: "empty_metrics_without_alerting",
			transforms: testf.TransformPipeline(
				withManagementState(operatorv1.Managed),
				withEmptyMetrics(),
				withNoAlerting(),
			),
			description: "Empty metrics should be allowed without alerting",
		},
		{
			name: "replicas_zero_without_storage",
			transforms: testf.TransformPipeline(
				withManagementState(operatorv1.Managed),
				testf.Transform(`.spec.monitoring.metrics = {"replicas": 0}`),
			),
			description: "Zero replicas should be allowed without storage",
		},
		{
			name: "replicas_with_storage",
			transforms: testf.TransformPipeline(
				withManagementState(operatorv1.Managed),
				withMetricsConfig(),
			),
			description: "Non-zero replicas should be allowed with storage",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.updateMonitoringConfig(testCase.transforms)
		})
	}
}

// ValidateOpenTelemetryCollectorConfigurations consolidates all OpenTelemetry Collector configuration tests.
func (tc *MonitoringTestCtx) ValidateOpenTelemetryCollectorConfigurations(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name       string
		transforms []testf.TransformFn
		validation gTypes.GomegaMatcher
	}{
		{
			name: "Basic Traces Configuration",
			transforms: []testf.TransformFn{
				withManagementState(operatorv1.Managed),
				withMonitoringTraces(TracesStorageBackendPV, "", "", TracesStorageRetention),
			},
			validation: jq.Match(`.spec.config.service.pipelines | has("traces")`),
		},
		{
			name: "Custom Metrics Exporters",
			transforms: []testf.TransformFn{
				withManagementState(operatorv1.Managed),
				withMetricsConfig(),
				withCustomMetricsExporters(),
			},
			validation: jq.Match(`
				(.spec.config.exporters | has("prometheus") and has("debug") and has("%s")) and
				(.spec.config.service.pipelines.metrics.exporters | length == 3 and contains(["prometheus", "debug", "%s"]))
			`, OtlpCustomExporter, OtlpCustomExporter),
		},
		// TODO: investigate why this test is passing locally but not on PRs
		/*		{
				name: "Custom Traces Exporters",
				transforms: []testf.TransformFn{
					withManagementState(operatorv1.Managed),
					withMonitoringTraces(TracesStorageBackendPV, "", "", ""),
					withCustomTracesExporters(),
				},
				validation: jq.Match(`
					(.spec.config.exporters | has("debug") and has("%s") and has("%s")) and
					(.spec.config.service.pipelines.traces.exporters | contains(["debug", "%s", "%s"]))
				`, OtlpHttpCustomExporter, OtlpTempoExporter, OtlpHttpCustomExporter, OtlpTempoExporter),
			},*/
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			// Ensure OpenTelemetry Collector is ready before each test
			tc.ensureOpenTelemetryCollectorReady(t)

			// Setup configuration
			tc.updateMonitoringConfig(testCase.transforms...)

			// Wait for the monitoring service to process the configuration
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
				WithCustomErrorMsg("Monitoring service should be ready before validating OpenTelemetry Collector"),
			)

			// Validate configuration
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
					Name:      OpenTelemetryCollectorName,
					Namespace: tc.MonitoringNamespace,
				}),
				WithCondition(testCase.validation),
			)

			// Universal cleanup to prevent state contamination between tests
			tc.cleanupAllMonitoringConfiguration()
		})
	}
}

func (tc *MonitoringTestCtx) ValidateMonitoringCRCollectorReplicas(t *testing.T) {
	t.Helper()

	const (
		defaultReplicas = 2
		testReplicas    = 3
	)

	// Setup monitoring configuration to allow collectorReplicas testing
	tc.updateMonitoringConfig(
		withManagementState(operatorv1.Managed),
		withMetricsConfig(),
	)

	monitoringCR := WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName})

	// Validate default collectorReplicas value
	tc.EnsureResourceExists(
		monitoringCR,
		WithCondition(jq.Match(`.spec.collectorReplicas == %d`, defaultReplicas)),
		WithCustomErrorMsg("CollectorReplicas should be set to the default value of %d", defaultReplicas),
	)

	// Update collectorReplicas to test value
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.collectorReplicas = %d`, testReplicas)),
	)

	// Validate collectorReplicas was updated by DSCInitialization controller
	tc.EnsureResourceExists(
		monitoringCR,
		WithCondition(jq.Match(`.spec.collectorReplicas == %d`, testReplicas)),
		WithCustomErrorMsg("CollectorReplicas should be updated to %d by DSCInitialization controller", testReplicas),
	)

	// Cleanup: Remove collectorReplicas to prevent test contamination
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(withNoCollectorReplicas()),
	)
}

// ValidateMonitoringCRDefaultTracesContent validates that traces stanza is omitted by default.
func (tc *MonitoringTestCtx) ValidateMonitoringCRDefaultTracesContent(t *testing.T) {
	t.Helper()

	// Ensure monitoring is enabled (might have been disabled by previous test)
	tc.updateMonitoringConfig(withManagementState(operatorv1.Managed))

	// Wait for the Monitoring resource to be created/updated and validate traces content
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			jq.Match(`.spec.traces == null`),
		)),
		WithCustomErrorMsg("Expected traces stanza to be omitted by default"),
	)
}

// ValidateTempoMonolithicCRCreation tests creation of TempoMonolithic CR with PV backend and custom retention.
func (tc *MonitoringTestCtx) ValidateTempoMonolithicCRCreation(t *testing.T) {
	t.Helper()

	// Update DSCI to set traces with PV backend
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			withMonitoringTraces(TracesStorageBackendPV, "", TracesStorageSize10Gi, TracesStorageRetention24h),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
		WithCondition(jq.Match(`.spec.traces != null`)),
		WithCustomErrorMsg("Monitoring resource should be updated with traces configuration by DSCInitialization controller"),
	)

	// Ensure the TempoMonolithic CR is created (status conditions are set by external tempo operator).
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.TempoMonolithic, types.NamespacedName{Name: TempoMonolithicName, Namespace: tc.MonitoringNamespace}),
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
	tc.cleanupTracesConfiguration()

	tc.DeleteResource(
		WithMinimalObject(gvk.TempoMonolithic, types.NamespacedName{Name: TempoMonolithicName, Namespace: tc.MonitoringNamespace}),
		WithWaitForDeletion(true),
	)
}

// ValidateTempoStackCRCreationWithCloudStorage tests creation of TempoStack CR with cloud storage backends.
func (tc *MonitoringTestCtx) ValidateTempoStackCRCreationWithCloudStorage(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name                string
		backend             string
		secretName          string
		monitoringCondition gTypes.GomegaMatcher
		monitoringErrorMsg  string
	}{
		{
			name:                "S3 backend",
			backend:             TracesStorageBackendS3,
			secretName:          TracesStorageBackendS3Secret,
			monitoringCondition: jq.Match(`.spec.traces != null`),
			monitoringErrorMsg:  "Monitoring resource should be updated with traces configuration by DSCInitialization controller",
		},
		{
			name:                "GCS backend",
			backend:             TracesStorageBackendGCS,
			secretName:          TracesStorageBackendGCSSecret,
			monitoringCondition: jq.Match(`.spec.traces.storage.backend == "%s"`, TracesStorageBackendGCS),
			monitoringErrorMsg:  "Monitoring resource should be updated with GCS traces configuration by DSCInitialization controller",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.validateTempoStackCreationWithBackend(
				t,
				testCase.backend,
				testCase.secretName,
				testCase.monitoringCondition,
				testCase.monitoringErrorMsg,
			)
		})
	}
}

// ValidateInstrumentationCRTracesWhenSet validates the content of the Instrumentation CR.
func (tc *MonitoringTestCtx) ValidateInstrumentationCRTracesWhenSet(t *testing.T) {
	t.Helper()

	// Ensure clean slate before starting
	tc.ensureMonitoringCleanSlate(t, "")

	// Update DSCI to set traces - ensure managementState remains Managed
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			withMonitoringTraces(TracesStorageBackendPV, "", "", ""),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
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
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: InstrumentationName, Namespace: tc.MonitoringNamespace}),
		WithCustomErrorMsg("Instrumentation CR should be created when traces are configured"),
	)
}

// ValidateInstrumentationCRTracesConfiguration validates the content of the Instrumentation CR with Traces.
func (tc *MonitoringTestCtx) ValidateInstrumentationCRTracesConfiguration(t *testing.T) {
	t.Helper()

	// Wait for the Instrumentation CR to be created and stabilized by the OpenTelemetry operator
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: InstrumentationName, Namespace: tc.MonitoringNamespace}),
		WithCondition(And(
			jq.Match(`.spec != null`),
			jq.Match(`.metadata.generation >= 1`),
		)),
		WithCustomErrorMsg("Instrumentation CR should be created and have a valid spec"),
	)

	// Fetch the Instrumentation CR and validate its content with Eventually for stability
	expectedEndpoint := fmt.Sprintf("http://%s.%s.svc.cluster.local:4317", OpenTelemetryCollectorName, tc.MonitoringNamespace)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: InstrumentationName, Namespace: tc.MonitoringNamespace}),
		WithCondition(
			jq.Match(`
				(.spec.exporter.endpoint == "%s") and
				(.spec.sampler.type == "traceidratio") and
				(.spec.sampler.argument == "0.1")
			`, expectedEndpoint),
		),
		WithCustomErrorMsg("Instrumentation CR should have the expected configuration"),
	)

	// Validate owner references
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Instrumentation, types.NamespacedName{Name: InstrumentationName, Namespace: tc.MonitoringNamespace}),
		WithCondition(monitoringOwnerReferencesCondition),
	)

	// Cleanup: Reset DSCInitialization traces configuration to prevent state contamination
	tc.cleanupTracesConfiguration()
}

func (tc *MonitoringTestCtx) ValidateTracesExportersReservedNameValidation(t *testing.T) {
	t.Helper()

	// Attempt to set traces configuration with a reserved exporter name
	tc.updateMonitoringConfig(
		withManagementState(operatorv1.Managed),
		withMonitoringTraces(TracesStorageBackendPV, "", "", ""), // Basic traces setup
		withReservedTracesExporter(),
	)

	// Validate that the Monitoring resource reports an error condition due to reserved name
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("reserved")`, status.ConditionTypeProvisioningSucceeded),
			),
		),
	)

	// Cleanup: Reset DSCInitialization traces configuration to prevent state contamination
	tc.cleanupTracesConfiguration()
}

// ValidatePrometheusRulesLifecycle validates that Prometheus rules are created when monitoring and dashboard are enabled, and deleted when both are disabled.
func (tc *MonitoringTestCtx) ValidatePrometheusRulesLifecycle(t *testing.T) {
	t.Helper()

	// Enable alerting + dashboard → Prometheus rules created
	tc.updateMonitoringConfig(
		withManagementState(operatorv1.Managed),
		withMetricsConfig(),
		withEmptyAlerting(),
	)
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.dashboard.managementState = "%s"`, operatorv1.Managed),
		)),
	)

	// Verify dashboard ready and both Prometheus rules exist
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Dashboard, types.NamespacedName{Name: "default-dashboard", Namespace: tc.AppsNamespace}),
		WithCondition(HaveLen(1)),
	)
	tc.EnsureResourceExists(WithMinimalObject(gvk.PrometheusRule, types.NamespacedName{Name: "dashboard-prometheusrules", Namespace: tc.MonitoringNamespace}))
	tc.EnsureResourceExists(WithMinimalObject(gvk.PrometheusRule, types.NamespacedName{Name: "operator-prometheusrules", Namespace: tc.MonitoringNamespace}))

	// Disable both dashboard and monitoring
	tc.updateMonitoringConfig(withManagementState(operatorv1.Removed))
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.dashboard.managementState = "%s"`, operatorv1.Removed)),
	)

	// Verify both Prometheus rules are deleted
	tc.EnsureResourceGone(WithMinimalObject(gvk.PrometheusRule, types.NamespacedName{Name: "dashboard-prometheusrules", Namespace: tc.MonitoringNamespace}))
	tc.EnsureResourceGone(WithMinimalObject(gvk.PrometheusRule, types.NamespacedName{Name: "operator-prometheusrules", Namespace: tc.MonitoringNamespace}))

	// Cleanup: Remove alerting configuration from DSCInitialization to prevent validation issues
	// This ensures that subsequent tests can set metrics=null without violating the validation rule
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(withNoAlerting()),
	)
}

// ValidateMonitoringServiceDisabled ensures complete monitoring service lifecycle from configuration removal to full disable.
func (tc *MonitoringTestCtx) ValidateMonitoringServiceDisabled(t *testing.T) {
	t.Helper()

	// Verify MonitoringStack CR is created (precondition for valid deletion test)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: MonitoringStackName, Namespace: tc.MonitoringNamespace}),
	)

	// Step 1: Remove metrics/alerting configuration (but keep monitoring enabled)
	// This should delete MonitoringStack but keep Monitoring CR
	tc.updateMonitoringConfig(
		withNamespace(tc.MonitoringNamespace),
		withManagementState(operatorv1.Managed), // Still managed
		withNoMetrics(),
		withNoAlerting(),
		withNoCollectorReplicas(), // Remove collectorReplicas since neither metrics nor traces are configured
	)

	// Verify MonitoringStack is deleted
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: MonitoringStackName, Namespace: tc.MonitoringNamespace}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithRemoveFinalizersOnDelete(true), // Remove finalizers if deletion is stuck
		WithCustomErrorMsg("MonitoringStack should be deleted when metrics and alerting are removed"),
	)

	// Verify Monitoring CR still exists with null metrics
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(jq.Match(`.spec.metrics == null`)),
		WithCustomErrorMsg("Monitoring CR should still exist with null metrics"),
	)

	// Step 2: Fully disable monitoring service
	tc.updateMonitoringConfig(withManagementState(operatorv1.Removed))

	// Verify Monitoring CR is deleted
	tc.EnsureResourcesGone(WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}))

	// Step 3: Comprehensive cleanup verification
	// Verify all monitoring-related resources are cleaned up
	for _, resource := range []struct {
		gvk  schema.GroupVersionKind
		name string
	}{
		{gvk.MonitoringStack, MonitoringStackName},
		{gvk.TempoStack, TempoStackName},
		{gvk.TempoMonolithic, TempoMonolithicName},
		{gvk.OpenTelemetryCollector, OpenTelemetryCollectorName},
		{gvk.Instrumentation, InstrumentationName},
	} {
		tc.EnsureResourcesGone(
			WithMinimalObject(resource.gvk, types.NamespacedName{
				Name:      resource.name,
				Namespace: tc.MonitoringNamespace,
			}),
			WithRemoveFinalizersOnDelete(true),
		)
	}
}

// ensureMonitoringCleanSlate ensures monitoring is completely removed before starting a test.
// This provides test isolation by guaranteeing each test starts with a clean state.
//
// Parameters:
//   - secretName: Optional secret name to clean up. If empty, only cleans default monitoring resources.
func (tc *MonitoringTestCtx) ensureMonitoringCleanSlate(t *testing.T, secretName string) {
	t.Helper()

	// Set monitoring to Removed to clean up all monitoring resources
	tc.updateMonitoringConfig(withManagementState(operatorv1.Removed))

	// Wait for all monitoring resources to be cleaned up
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithRemoveFinalizersOnDelete(true), // Remove finalizers just in case it stuck.
	)

	// Clean up TempoStack and associated secret (if provided)
	tc.cleanupTempoStackAndSecret(secretName)
}

// ensureOpenTelemetryCollectorReady waits for the OpenTelemetry Collector deployment to be ready
// before proceeding with further validation. This ensures that at least one replica is running.
func (tc *MonitoringTestCtx) ensureOpenTelemetryCollectorReady(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{Name: OpenTelemetryCollectorName, Namespace: tc.MonitoringNamespace}),
		// Format of statusReplicas is n/m, we check if at least one is ready
		WithCondition(jq.Match(`.status.scale.statusReplicas | split("/") | map(tonumber) | min > 0`)),
		WithCustomErrorMsg("OpenTelemetry Collector should have at least one ready replica"),
	)
}

// cleanupTempoStackAndSecret removes TempoStack and optionally associated secret resources.
//
// Parameters:
//   - secretName: Optional name of the secret to delete alongside the TempoStack. If empty, only TempoStack is deleted.
func (tc *MonitoringTestCtx) cleanupTempoStackAndSecret(secretName string) {
	tc.DeleteResource(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{
			Name:      TempoStackName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithWaitForDeletion(false),
		WithRemoveFinalizersOnDelete(true),
		WithIgnoreNotFound(true),
	)

	// Only delete secret if one is specified
	if secretName != "" {
		tc.DeleteResource(
			WithMinimalObject(gvk.Secret, types.NamespacedName{
				Name:      secretName,
				Namespace: tc.MonitoringNamespace,
			}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	}
}

// validateTempoStackCreationWithBackend validates TempoStack creation with the specified backend.
// It ensures a clean monitoring state, creates the secret, enables monitoring with traces
// configuration, and validates the TempoStack creation.
//
// Parameters:
//   - backend: The storage backend type (e.g., "s3", "gcs")
//   - secretName: The name of the secret containing backend credentials
//   - monitoringCondition: Gomega matcher to validate the Monitoring resource state
//   - monitoringErrorMsg: Error message to display if Monitoring resource validation fails
func (tc *MonitoringTestCtx) validateTempoStackCreationWithBackend(
	t *testing.T,
	backend, secretName string,
	monitoringCondition gTypes.GomegaMatcher,
	monitoringErrorMsg string,
) {
	t.Helper()

	t.Logf("Starting validateTempoStackCreationWithBackend for backend=%s, secretName=%s", backend, secretName)

	// Ensure clean slate before starting this validation
	tc.ensureMonitoringCleanSlate(t, secretName)

	// Create the secret before enabling monitoring
	t.Logf("Creating secret %s in namespace %s", secretName, tc.MonitoringNamespace)
	tc.createDummySecret(backend, secretName, tc.MonitoringNamespace)

	// Now update DSCI to set traces with specified backend
	t.Logf("Updating DSCI with backend=%s, secretName=%s", backend, secretName)
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			withMonitoringTraces(backend, secretName, "", DefaultRetention),
		)),
	)

	// Wait for the Monitoring resource to be updated by DSCInitialization controller
	t.Logf("Waiting for Monitoring resource to be updated")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(monitoringCondition),
		WithCustomErrorMsg(monitoringErrorMsg),
	)

	// Ensure the TempoStack CR is created with specified backend
	// (status conditions are set by external tempo operator)
	t.Logf("Validating TempoStack creation with backend=%s, secretName=%s", backend, secretName)
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.TempoStack, types.NamespacedName{
			Name:      TempoStackName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCondition(
			And(
				// Validate the backend is set correctly
				jq.Match(`.spec.storage.secret.type == "%s"`, backend),
				jq.Match(`.spec.storage.secret.name == "%s"`, secretName),
				// Validate retention is set correctly
				jq.Match(`.spec.retention.global.traces == "%s"`, FormattedRetention), // to match 100m
				// Validate that the Tempo operator has accepted and reconciled the resource
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			),
		),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithCustomErrorMsg("TempoStack should be created with %s backend, but was not found or has incorrect backend type", backend),
	)

	// Cleanup: Reset DSCInitialization traces configuration, delete TempoStack and secret
	// This ensures proper test isolation and prevents state contamination between tests
	t.Logf("Cleanup traces configuration and TempoStack")
	tc.cleanupTracesConfiguration()

	t.Logf("Cleaning up TempoStack and secret for backend=%s", backend)
	tc.cleanupTempoStackAndSecret(secretName)

	t.Logf("Cleanup completed for backend=%s", backend)
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

// cleanupTracesConfiguration resets DSCInitialization traces configuration to prevent state contamination.
func (tc *MonitoringTestCtx) cleanupTracesConfiguration() {
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(withNoTraces()),
	)
}

// cleanupAllMonitoringConfiguration completely removes ALL monitoring configuration to prevent state contamination between tests.
// This function recreates the monitoring configuration from scratch with only the management state set.
func (tc *MonitoringTestCtx) cleanupAllMonitoringConfiguration() {
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring = {"managementState": "%s"}`, operatorv1.Managed)),
	)
}

// updateMonitoringConfig provides a flexible way to update DSCI monitoring configuration.
func (tc *MonitoringTestCtx) updateMonitoringConfig(transforms ...testf.TransformFn) {
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(transforms...)),
	)
}

// Helper functions for common monitoring configuration patterns

// withManagementState returns a transform that sets managementState to the specified state.
func withManagementState(state operatorv1.ManagementState) testf.TransformFn {
	return testf.Transform(`.spec.monitoring.managementState = "%s"`, state)
}

// withMetricsConfig returns a single transform for setting up metrics configuration using pipeline.
func withMetricsConfig() testf.TransformFn {
	return testf.Transform(`.spec.monitoring.metrics = {
        "storage": {
            "size": "%s",
            "retention": "%s"
        },
        "resources": {
            "cpurequest": "%s",
            "memoryrequest": "%s"
        },
        "replicas": %d
    }`, MetricsStorageSize, MetricsRetention, MetricsCPURequest, MetricsMemoryRequest, MetricsDefaultReplicas)
}

// withMetricsReplicas returns a transform that sets metrics replicas.
func withMetricsReplicas(replicas int) testf.TransformFn {
	return testf.Transform(`.spec.monitoring.metrics.replicas = %d`, replicas)
}

// withNamespace returns a transform that sets monitoring namespace.
func withNamespace(namespace string) testf.TransformFn {
	return testf.Transform(`.spec.monitoring.namespace = "%s"`, namespace)
}

// withEmptyMetrics returns a transform that clears metrics configuration.
func withEmptyMetrics() testf.TransformFn {
	return testf.Transform(`.spec.monitoring.metrics = {}`)
}

// withEmptyAlerting returns a transform that clears alerting configuration.
func withEmptyAlerting() testf.TransformFn {
	return testf.Transform(`.spec.monitoring.alerting = {}`)
}

// withNoMetrics returns a transform that removes the metrics field entirely.
func withNoMetrics() testf.TransformFn {
	return testf.Transform(`del(.spec.monitoring.metrics)`)
}

// withNoAlerting returns a transform that removes the alerting field entirely.
func withNoAlerting() testf.TransformFn {
	return testf.Transform(`del(.spec.monitoring.alerting)`)
}

// withNoTraces returns a transform that removes the traces field entirely.
func withNoTraces() testf.TransformFn {
	return testf.Transform(`del(.spec.monitoring.traces)`)
}

// withNoCollectorReplicas returns a transform that removes the collectorReplicas field entirely.
func withNoCollectorReplicas() testf.TransformFn {
	return testf.Transform(`del(.spec.monitoring.collectorReplicas)`)
}

// withCustomMetricsExporters returns a transform that sets custom metrics exporters.
func withCustomMetricsExporters() testf.TransformFn {
	return testf.Transform(`.spec.monitoring.metrics.exporters = {
		"debug": "verbosity: detailed",
        "%s": "endpoint: http://custom-backend:4317\ntls:\n  insecure: true"
	}`, OtlpCustomExporter)
}

// withCustomTracesExporters returns a transform that sets custom traces exporters for testing.
// TODO: remove the //nolint:unused when identified the issue with the related test.
func withCustomTracesExporters() testf.TransformFn { //nolint:unused
	return testf.Transform(`.spec.monitoring.traces.exporters = {
        "debug": {
            "verbosity": "detailed"
        },
        "%s": {
            "endpoint": "http://custom-endpoint:4318",
            "headers": {
                "api-key": "secret-key"
            }
        }
    }`, OtlpHttpCustomExporter)
}

// withReservedTracesExporter returns a transform that sets a reserved exporter name for validation testing.
func withReservedTracesExporter() testf.TransformFn {
	return testf.Transform(`.spec.monitoring.traces.exporters = {
        "%s": {
            "endpoint": "http://malicious-endpoint:4317"
        }
    }`, OtlpTempoExporter)
}

// withMonitoringTraces configures traces settings in the DSCI monitoring spec.
func withMonitoringTraces(backend, secret, size, retention string) testf.TransformFn {
	transforms := []testf.TransformFn{
		testf.Transform(`.spec.monitoring.traces = {
        "storage": {
            "backend": "%s"
        },
        "exporters": null

    }`, backend),
	}

	// Handle secret field: set to specific value if provided, otherwise clear it
	if secret != "" {
		transforms = append(transforms, testf.Transform(`.spec.monitoring.traces.storage.secret = "%s"`, secret))
	}

	// Handle retention field: set to specific value if provided, otherwise clear it
	if retention != "" {
		transforms = append(transforms, testf.Transform(`.spec.monitoring.traces.storage.retention = "%s"`, retention))
	}

	// Handle size field: set to specific value for pv backend, otherwise clear it
	if backend == TracesStorageBackendPV && size != "" {
		transforms = append(transforms, testf.Transform(`.spec.monitoring.traces.storage.size = "%s"`, size))
	}

	return testf.TransformPipeline(transforms...)
}

// validatePrometheusRestrictedResourcesCommon validates common prometheus-restricted resources that are shared between tests.
func (tc *MonitoringTestCtx) validatePrometheusRestrictedResourcesCommon(t *testing.T, namespace string) {
	t.Helper()

	// Verify the prometheus-restricted deployment is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "prometheus-restricted",
			Namespace: namespace,
		}),
		WithCondition(And(
			jq.Match(`.status.readyReplicas == 1`),
			jq.Match(`.spec.template.spec.containers | length == 2`), // kube-rbac-proxy + prom-label-proxy
		)),
		WithCustomErrorMsg("prometheus-restricted deployment should be created and ready"),
	)

	// Verify the service account exists with proper RBAC
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{
			Name:      "prometheus-restricted",
			Namespace: namespace,
		}),
	)

	// Verify the ClusterRoleBinding exists for prometheus metrics reader
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{
			Name: "prometheus-restricted",
		}),
		WithCondition(And(
			jq.Match(`.roleRef.name == "odh:prometheus-metrics-reader"`),
			jq.Match(`.subjects[0].name == "prometheus-restricted"`),
			jq.Match(`.subjects[0].namespace == "%s"`, namespace),
		)),
	)

	// Verify the service is created with proper annotations for TLS
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      "prometheus-restricted",
			Namespace: namespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.ports[0].port == 8443`),
			jq.Match(`.spec.ports[0].name == "https"`),
			jq.Match(`.metadata.annotations."service.beta.openshift.io/serving-cert-secret-name" == "prometheus-restricted-tls"`),
		)),
	)

	// Verify the route is created for external access
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Route, types.NamespacedName{
			Name:      "data-science-prometheus-route",
			Namespace: namespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.to.name == "prometheus-restricted"`),
			jq.Match(`.spec.tls.termination == "reencrypt"`),
			jq.Match(`.spec.tls.insecureEdgeTerminationPolicy == "Redirect"`),
		)),
	)
}

// ValidatePrometheusRestrictedResourceConfiguration tests the namespace-restricted metrics access functionality.
func (tc *MonitoringTestCtx) ValidatePrometheusRestrictedResourceConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Ensure metrics are configured
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			withMetricsConfig(),
		)),
	)

	// Validate common prometheus-restricted resources
	tc.validatePrometheusRestrictedResourcesCommon(t, dsci.Spec.Monitoring.Namespace)
}

// ValidatePrometheusSecureProxyAuthentication tests the Prometheus secure proxy authentication and authorization.
func (tc *MonitoringTestCtx) ValidatePrometheusSecureProxyAuthentication(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Validate common prometheus-restricted resources
	tc.validatePrometheusRestrictedResourcesCommon(t, dsci.Spec.Monitoring.Namespace)

	// Verify the prometheus-restricted deployment contains kube-rbac-proxy with specific details
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "prometheus-restricted",
			Namespace: dsci.Spec.Monitoring.Namespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.template.spec.containers[0].name == "kube-rbac-proxy"`),
			jq.Match(`.spec.template.spec.containers[0].image | contains("kube-rbac-proxy")`),
		)),
		WithCustomErrorMsg("prometheus-restricted deployment should contain kube-rbac-proxy container"),
	)

	// Verify the auth-delegator ClusterRoleBinding exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{
			Name: "prometheus-restricted-auth-delegator",
		}),
		WithCondition(And(
			jq.Match(`.roleRef.name == "system:auth-delegator"`),
			jq.Match(`.subjects[0].name == "prometheus-restricted"`),
			jq.Match(`.subjects[0].namespace == "%s"`, dsci.Spec.Monitoring.Namespace),
		)),
	)

	// Verify the ConfigMap for kube-rbac-proxy configuration exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      "prometheus-restricted-config",
			Namespace: dsci.Spec.Monitoring.Namespace,
		}),
		WithCondition(jq.Match(`.data."kube-rbac-proxy.yaml" | contains("authorization")`)),
	)

	// Verify that the kube-rbac-proxy container has the correct upstream configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "prometheus-restricted",
			Namespace: dsci.Spec.Monitoring.Namespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.template.spec.containers[0].args | map(select(contains("--upstream="))) | length == 1`),
			jq.Match(`.spec.template.spec.containers[0].args | map(select(contains("--upstream=http://127.0.0.1:9091/"))) | length == 1`),
			jq.Match(`.spec.template.spec.containers[0].args | map(select(contains("--secure-listen-address=0.0.0.0:8443"))) | length == 1`),
		)),
		WithCustomErrorMsg("kube-rbac-proxy should be configured with correct upstream and secure listen address"),
	)

	// Verify that the ClusterRole for prometheus metrics reader exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRole, types.NamespacedName{
			Name: "odh:prometheus-metrics-reader",
		}),
		WithCondition(And(
			jq.Match(`.rules | map(select(.apiGroups | contains(["authentication.k8s.io"]))) | length >= 1`),
			jq.Match(`.rules | map(select(.apiGroups | contains(["authorization.k8s.io"]))) | length >= 1`),
			jq.Match(`.rules | map(select(.resources | contains(["tokenreviews"]))) | length >= 1`),
			jq.Match(`.rules | map(select(.resources | contains(["subjectaccessreviews"]))) | length >= 1`),
		)),
		WithCustomErrorMsg("ClusterRole should have proper permissions for authentication and authorization"),
	)
}

// ValidateMetricsAPIAuthenticationAndAuthorization tests comprehensive authentication and authorization scenarios
// for the namespace-restricted metrics API endpoint.
func (tc *MonitoringTestCtx) ValidateMetricsAPIAuthenticationAndAuthorization(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.monitoring.managementState = "%s"`, operatorv1.Managed),
			withMetricsConfig(),
		)),
	)

	// Wait for the MonitoringStack to be ready first (which creates the route)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: MonitoringStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeAvailable, metav1.ConditionTrue)),
		WithCustomErrorMsg("MonitoringStack should be ready before testing namespace-restricted metrics"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      "data-science-monitoringstack-prometheus",
			Namespace: dsci.Spec.Monitoring.Namespace,
		}),
		WithCustomErrorMsg("data-science-monitoringstack-prometheus service should be created by MonitoringStack operator before testing"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "prometheus-restricted",
			Namespace: dsci.Spec.Monitoring.Namespace,
		}),
		WithCondition(jq.Match(`.status.readyReplicas == 1`)),
		WithCustomErrorMsg("prometheus-restricted deployment should be ready before testing"),
	)

	// Get the route URL for the prometheus-restricted endpoint
	routeURL := tc.getNamespaceRestrictedMetricsRouteURL(t, dsci.Spec.Monitoring.Namespace)

	// Test 1: Valid user with proper authorization can query namespace-scoped metrics
	t.Run("Valid user with proper authz accesses namespace-scoped metrics", func(t *testing.T) {
		tc.testValidUserWithProperAuthz(t, routeURL, dsci.Spec.Monitoring.Namespace)
	})

	// Test 2: Valid user with improper authorization gets denied access
	t.Run("Valid user with improper authz gets denied access", func(t *testing.T) {
		tc.testValidUserWithImproperAuthz(t, routeURL, dsci.Spec.Monitoring.Namespace)
	})

	// Test 3: Invalid authentication gets denied access
	t.Run("Invalid authentication gets denied access", func(t *testing.T) {
		tc.testInvalidAuthentication(t, routeURL)
	})
}

// getNamespaceRestrictedMetricsRouteURL retrieves the URL for the data-science-prometheus-route.
func (tc *MonitoringTestCtx) getNamespaceRestrictedMetricsRouteURL(t *testing.T, namespace string) string {
	t.Helper()

	var route routev1.Route
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Route, types.NamespacedName{
			Name:      "data-science-prometheus-route",
			Namespace: namespace,
		}),
		WithCondition(jq.Match(`.spec.host != ""`)),
		WithCustomErrorMsg("data-science-prometheus-route should have a valid host"),
	)

	err := tc.Client().Get(tc.Context(), types.NamespacedName{
		Name:      "data-science-prometheus-route",
		Namespace: namespace,
	}, &route)
	require.NoError(t, err, "Failed to get data-science-prometheus-route")

	return fmt.Sprintf("https://%s", route.Spec.Host)
}

// testValidUserWithProperAuthz tests that a valid user with proper authorization can access namespace-scoped metrics.
func (tc *MonitoringTestCtx) testValidUserWithProperAuthz(t *testing.T, routeURL, namespace string) {
	t.Helper()

	testSA := tc.createTestServiceAccount(t, "test-metrics-admin", namespace)
	testGroup := "test-metrics-admins"
	tc.createTestGroup(t, testGroup, testSA.Name, namespace)
	tc.addGroupToMetricsAdminGroups(t, testGroup)
	tc.waitForRBACPropagation(t, testGroup)
	token := tc.getServiceAccountToken(t, testSA.Name, namespace)
	tc.testMetricsAccess(t, routeURL, token, namespace, true, "Valid user with proper authz should be able to access namespace-scoped metrics")
	tc.cleanupTestUser(t, testSA.Name, namespace, testGroup)
}

// testValidUserWithImproperAuthz tests that a valid user without proper authorization gets denied access.
func (tc *MonitoringTestCtx) testValidUserWithImproperAuthz(t *testing.T, routeURL, namespace string) {
	t.Helper()

	// Create a test service account for the user (not in metrics admin group)
	testSA := tc.createTestServiceAccount(t, "test-regular-user", namespace)
	token := tc.getServiceAccountToken(t, testSA.Name, namespace)
	tc.testMetricsAccess(t, routeURL, token, namespace, false, "Valid user without proper authz should be denied access to metrics")
	tc.cleanupTestUser(t, testSA.Name, namespace, "")
}

// testInvalidAuthentication tests that invalid authentication gets denied access.
func (tc *MonitoringTestCtx) testInvalidAuthentication(t *testing.T, routeURL string) {
	t.Helper()
	tc.testMetricsAccess(t, routeURL, "invalid-token", "test-namespace", false, "Invalid authentication should be denied access")
	tc.testMetricsAccess(t, routeURL, "", "test-namespace", false, "Request without authentication should be denied access")
}

// createTestServiceAccount creates a test service account for authentication testing.
func (tc *MonitoringTestCtx) createTestServiceAccount(t *testing.T, name, namespace string) *corev1.ServiceAccount {
	t.Helper()

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"test.opendatahub.io/metrics-auth": "true",
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(sa))
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{Name: name, Namespace: namespace}),
		WithCustomErrorMsg("Test service account should be created"),
	)

	return sa
}

// createTestGroup creates a test OpenShift group and adds the service account to it.
func (tc *MonitoringTestCtx) createTestGroup(t *testing.T, groupName, serviceAccountName, namespace string) {
	t.Helper()

	// OpenShift requires usernames with ":" to be base64 encoded with "b64:" prefix
	serviceAccountUserName := fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName)
	encodedUserName := "b64:" + base64.StdEncoding.EncodeToString([]byte(serviceAccountUserName))

	user := &userv1.User{
		ObjectMeta: metav1.ObjectMeta{
			Name: encodedUserName,
			Labels: map[string]string{
				"test.opendatahub.io/metrics-auth": "true",
			},
		},
		Groups: []string{groupName},
	}

	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(user))
}

// addGroupToMetricsAdminGroups adds a group to the MetricsAdminGroups in the Auth CR.
func (tc *MonitoringTestCtx) addGroupToMetricsAdminGroups(t *testing.T, groupName string) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Auth, types.NamespacedName{Name: serviceApi.AuthInstanceName}),
		WithMutateFunc(testf.Transform(`.spec.metricsAdminGroups += ["%s"]`, groupName)),
		WithCustomErrorMsg("Failed to add group to MetricsAdminGroups"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Auth, types.NamespacedName{Name: serviceApi.AuthInstanceName}),
		WithCondition(jq.Match(`.spec.metricsAdminGroups | index("%s") != null`, groupName)),
		WithCustomErrorMsg("Group should be added to MetricsAdminGroups"),
	)
}

// waitForRBACPropagation waits for RBAC changes to propagate through the system.
func (tc *MonitoringTestCtx) waitForRBACPropagation(t *testing.T, groupName string) {
	t.Helper()

	// Wait for the ClusterRoleBinding to be created for the metrics admin group
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{Name: "data-science-metrics-admin-clusterrolebinding"}),
		WithCondition(jq.Match(`.subjects | map(.name) | index("%s") != null`, groupName)),
		WithCustomErrorMsg("ClusterRoleBinding should include the test group"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	// Give additional time for RBAC to propagate to the API server
	time.Sleep(5 * time.Second)
}

// getServiceAccountToken retrieves the token for a service account.
func (tc *MonitoringTestCtx) getServiceAccountToken(t *testing.T, serviceAccountName, namespace string) string {
	t.Helper()

	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: func() *int64 { i := int64(3600); return &i }(), // 1 hour
		},
	}

	ctx := tc.Context()
	err := tc.Client().SubResource("token").Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: namespace,
		},
	}, tokenRequest)

	require.NoError(t, err, "Failed to create token for service account")
	require.NotEmpty(t, tokenRequest.Status.Token, "Token should not be empty")

	return tokenRequest.Status.Token
}

// testMetricsAccess tests access to the metrics endpoint with the given token.
func (tc *MonitoringTestCtx) testMetricsAccess(t *testing.T, routeURL, token, namespace string, expectSuccess bool, errorMsg string) {
	t.Helper()

	// Create HTTP client with custom transport to skip TLS verification for tests
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - Required for testing with self-signed certificates
	}
	client := &http.Client{Transport: tr}
	requestURL := fmt.Sprintf("%s/api/v1/query?query=up&namespace=%s", routeURL, namespace)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, requestURL, nil)
	require.NoError(t, err, "Failed to create HTTP request")

	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	// Make the request
	resp, err := client.Do(req)
	require.NoError(t, err, "Failed to make HTTP request")
	defer resp.Body.Close()

	if expectSuccess {
		tc.g.Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusBadRequest)), errorMsg+": Expected success status code (200 or 400 for query errors)")
		// If we get 400, it might be a query error but authentication/authorization succeeded
		if resp.StatusCode == http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Got 400 response (query error but auth succeeded): %s", string(body))
		}
	} else {
		tc.g.Expect(resp.StatusCode).To(Or(Equal(http.StatusUnauthorized), Equal(http.StatusForbidden)), errorMsg+": Expected authentication/authorization failure status code")
	}

	t.Logf("Metrics access test completed: URL=%s, Token=%s, ExpectSuccess=%t, StatusCode=%d",
		requestURL, token[:min(10, len(token))]+"...", expectSuccess, resp.StatusCode)
}

// cleanupTestUser removes test user resources.
func (tc *MonitoringTestCtx) cleanupTestUser(t *testing.T, serviceAccountName, namespace, groupName string) {
	t.Helper()

	tc.DeleteResource(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{Name: serviceAccountName, Namespace: namespace}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)

	// Remove user if it was created (need to use the same base64 encoded name)
	serviceAccountUserName := fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName)
	encodedUserName := "b64:" + base64.StdEncoding.EncodeToString([]byte(serviceAccountUserName))
	userGVK := schema.GroupVersionKind{
		Group:   userv1.SchemeGroupVersion.Group,
		Version: userv1.SchemeGroupVersion.Version,
		Kind:    "User",
	}
	tc.DeleteResource(
		WithMinimalObject(userGVK, types.NamespacedName{Name: encodedUserName}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)

	if groupName != "" {
		tc.EventuallyResourceCreatedOrUpdated(
			WithMinimalObject(gvk.Auth, types.NamespacedName{Name: serviceApi.AuthInstanceName}),
			WithMutateFunc(testf.Transform(`.spec.metricsAdminGroups = (.spec.metricsAdminGroups // []) - ["%s"]`, groupName)),
			WithIgnoreNotFound(true),
		)
	}
}
