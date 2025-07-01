package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type MonitoringTestCtx struct {
	*TestContext
}

// setupMonitoringCRDs ensures that all required CRDs for monitoring tests are installed.
func (tc *MonitoringTestCtx) setupMonitoringCRDs() {
	// Install MonitoringStack CRD
	if !tc.isMonitoringStackCRDAvailable() {
		tc.createMonitoringStackCRD()
	}
}

// createMonitoringStackCRD creates a mock MonitoringStack CRD.
func (tc *MonitoringTestCtx) createMonitoringStackCRD() {
	crd := tc.createMockCRD(gvk.MonitoringStack, "monitoring")
	tc.EnsureResourceCreatedOrUpdated(WithObjectToCreate(crd))
}

// createMockCRD creates a mock CRD for a given group, version, kind, and component.
func (tc *MonitoringTestCtx) createMockCRD(gvk schema.GroupVersionKind, componentName string) *apiextv1.CustomResourceDefinition {
	return &apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: strings.ToLower(fmt.Sprintf("%ss.%s", gvk.Kind, gvk.Group)),
			Labels: map[string]string{
				labels.ODH.Component(componentName): labels.True,
			},
		},
		Spec: apiextv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: apiextv1.CustomResourceDefinitionNames{
				Kind:   gvk.Kind,
				Plural: strings.ToLower(gvk.Kind) + "s",
			},
			Scope: apiextv1.NamespaceScoped,
			Versions: []apiextv1.CustomResourceDefinitionVersion{
				{
					Name:    gvk.Version,
					Served:  true,
					Storage: true,
					Schema: &apiextv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextv1.JSONSchemaProps{
										"alertmanagerConfig": {Type: "object"},
										"logLevel":           {Type: "string"},
										"prometheusConfig":   {Type: "object"},
										"resourceSelector":   {Type: "object"},
										"resources":          {Type: "object"},
										"retention":          {Type: "string"},
									},
								},
								"status": {
									Type: "object",
									Properties: map[string]apiextv1.JSONSchemaProps{
										"conditions": {
											Type: "array",
											Items: &apiextv1.JSONSchemaPropsOrArray{
												Schema: &apiextv1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextv1.JSONSchemaProps{
														"type":   {Type: "string"},
														"status": {Type: "string"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					Subresources: &apiextv1.CustomResourceSubresources{
						Status: &apiextv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}
}

// isMonitoringStackCRDAvailable checks if the MonitoringStack CRD is available in the cluster.
func (tc *MonitoringTestCtx) isMonitoringStackCRDAvailable() bool {
	exists, err := cluster.HasCRD(context.Background(), tc.g.Client(), gvk.MonitoringStack)
	if err != nil {
		return false
	}
	return exists
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

	// Setup required CRDs for monitoring tests
	monitoringServiceCtx.setupMonitoringCRDs()

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

	comp := serviceApi.MonitoringSpec{
		MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{Namespace: "opendatahub", Metrics: nil},
	}
	// Validate the metrics stanza is omitted by default
	tc.g.Expect(monitoring.Spec.Metrics).
		To(Equal(comp.Metrics),
			"Expected metrics stanza to be omitted by default")

	// Validate MontoringStack CR is not created
	monitoringStackName := getMonitoringStackName(dsci)
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: monitoringStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsWhenSet(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	monitoringStackName := getMonitoringStackName(dsci)

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

	// ensure the MonitoringStack CR is created with Available status
	ms := tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: monitoringStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeAvailable, metav1.ConditionTrue)),
	)
	tc.g.Expect(ms).ToNot(BeNil())
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsConfiguration(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	monitoringStackName := getMonitoringStackName(dsci)

	// Use EnsureResourceExists with jq matchers for cleaner validation
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: monitoringStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
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
		WithCustomErrorMsg("MonitoringStack '%s' configuration validation failed", monitoringStackName),
	)
}

func (tc *MonitoringTestCtx) ValidateMonitoringStackCRMetricsReplicasUpdate(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	monitoringStackName := getMonitoringStackName(dsci)

	// Update DSCI to set replicas to 1 (must include either storage or resources due to CEL validation rule)
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.monitoring.metrics = %s`, `{storage: {size: "5Gi", retention: "1d"}, replicas: 1}`)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MonitoringStack, types.NamespacedName{Name: monitoringStackName, Namespace: dsci.Spec.Monitoring.Namespace}),
		WithCondition(And(
			// Validate storage size is still the same value
			jq.Match(`.spec.prometheusConfig.persistentVolumeClaim.resources.requests.storage == "%s"`, "5Gi"),
			// Validate replicas is set to 1 when it is updated in DSCI
			jq.Match(`.spec.prometheusConfig.replicas == %d`, 1),
		)),
		WithCustomErrorMsg("MonitoringStack '%s' configuration validation failed", monitoringStackName),
	)
}

func getMonitoringStackName(dsci *dsciv1.DSCInitialization) string {
	switch dsci.Status.Release.Name {
	case cluster.ManagedRhoai, cluster.SelfManagedRhoai:
		return "rhoai-monitoringstack"
	}

	return "odh-monitoringstack"
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

// ValidateMonitoringCRDefaultTracesContent validates that traces stanza is omitted by default.
func (tc *MonitoringTestCtx) ValidateMonitoringCRDefaultTracesContent(t *testing.T) {
	t.Helper()

	// Ensure that the Monitoring resource exists.
	monitoring := &serviceApi.Monitoring{}
	tc.FetchTypedResource(monitoring, WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: "default-monitoring"}))

	// Validate the traces stanza is omitted by default
	tc.g.Expect(monitoring.Spec.Traces).
		To(BeNil(),
			"Expected traces stanza to be omitted by default")
}
