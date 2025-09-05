package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// ServiceMesh test configuration constants and variables.
// These define default names, namespaces, and expected values for ServiceMesh-related resources.
const (
	// ServiceMesh Control Plane configuration.
	serviceMeshControlPlaneDefaultName = "data-science-smcp" // Default SMCP instance name
	serviceMeshControlPlaneNamespace   = "istio-system"      // Standard Istio control plane namespace

	// Authorino authorization service configuration.
	authorinoDefaultName         = "authorino"                      // Default Authorino instance name
	authorinoDefaultNamespace    = "opendatahub-auth-provider"      // Authorino deployment namespace
	defaultAuthAudience          = "https://kubernetes.default.svc" // Default JWT audience for auth
	serviceMeshMemberDefaultName = "default"                        // Default ServiceMeshMember name

	// ServiceMesh metrics and operator configuration.
	serviceMeshMetricsCollectionDefault = "Istio" // Default metrics collection backend
)

var (
	// These avoid repetitive construction throughout the test suite.
	namespacedServiceMeshControlPlane = types.NamespacedName{
		Name:      serviceMeshControlPlaneDefaultName,
		Namespace: serviceMeshControlPlaneNamespace,
	}

	namespacedServiceMeshMember = types.NamespacedName{
		Name:      serviceMeshMemberDefaultName,
		Namespace: authorinoDefaultNamespace,
	}

	namespacedAuthorino = types.NamespacedName{
		Name:      authorinoDefaultName,
		Namespace: authorinoDefaultNamespace,
	}

	// These are dynamically generated based on the control plane name.
	serviceMonitorDefaultName = fmt.Sprintf("%s-pilot-monitor", serviceMeshControlPlaneDefaultName)
	namespacedServiceMonitor  = types.NamespacedName{
		Name:      serviceMonitorDefaultName,
		Namespace: serviceMeshControlPlaneNamespace,
	}

	podMonitorDefaultName = fmt.Sprintf("%s-envoy-monitor", serviceMeshControlPlaneDefaultName)
	namespacedPodMonitor  = types.NamespacedName{
		Name:      podMonitorDefaultName,
		Namespace: serviceMeshControlPlaneNamespace,
	}

	// These verify that DSCI is available and reconciliation is complete.
	dsciAvailableConditions = []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "%s"`, metav1.ConditionTrue),
		jq.Match(`.status.conditions[] | select(.type == "ReconcileComplete") | .status == "%s"`, metav1.ConditionTrue),
	}

	// Common ServiceMesh resource validation conditions.
	serviceMeshReadyConditions = []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
	}

	// Common DependentOperatorsTestConfig patterns used throughout the test suite.
	BothOperatorsEnabled = DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	}

	NoOperatorsEnabled = DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: false,
		EnsureAuthorinoOperatorInstalled:   false,
	}

	OnlyServiceMeshEnabled = DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   false,
	}
)

// DependentOperatorsTestConfig is used to configure the test cases that depend on the presence of dependent operators.
// Allows to set which dependent operators will or won't be installed for the test case.
type DependentOperatorsTestConfig struct {
	EnsureServiceMeshOperatorInstalled bool
	EnsureAuthorinoOperatorInstalled   bool
}

type ServiceMeshTestCtx struct {
	*TestContext
	GVK            schema.GroupVersionKind
	NamespacedName types.NamespacedName
}

// serviceMeshControllerTestSuite runs the complete ServiceMesh integration test suite.
func serviceMeshControllerTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	smCtx := ServiceMeshTestCtx{
		TestContext: tc,
		GVK:         gvk.ServiceMesh,
		NamespacedName: types.NamespacedName{
			Name: serviceApi.ServiceMeshInstanceName,
		},
	}

	// Define test cases.
	testCases := []TestCase{
		// Test cases to validate default ServiceMesh CR instance-related flow
		{"Validate ServiceMesh initialization and setup", smCtx.ValidateServiceMeshInitialization},
		{"Validate ServiceMesh control plane", smCtx.ValidateServiceMeshControlPlane},
		{"Validate Authorino resources", smCtx.ValidateAuthorinoResources},
		{"Validate ServiceMesh metrics collection resources", smCtx.ValidateServiceMeshMetricsCollectionResources},
		{"Validate ServiceMesh metrics collection disabled", smCtx.ValidateServiceMeshMetricsCollectionDisabled},
		// state transition tests
		{"Validate ServiceMesh transition to unmanaged", smCtx.ValidateServiceMeshTransitionToUnmanaged},
		{"Validate ServiceMesh transition to removed", smCtx.ValidateServiceMeshTransitionToRemoved},
		// test removal scenario of Legacy ServiceMesh-related FeatureTrackers being present
		{"Validate Legacy ServiceMesh FeatureTrackers removal", smCtx.ValidateLegacyServiceMeshFeatureTrackersRemoval},
		// test cases for missing dependent operators
		{"Validate Authorino operator not installed", smCtx.ValidateAuthorinoOperatorNotInstalled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateServiceMeshInitialization verifies ServiceMesh setup, singleton pattern, and absence of legacy FeatureTrackers.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshInitialization(t *testing.T) {
	t.Helper()

	// Setup environment with both operators
	tc.setupAndValidateServiceMeshEnvironment(t, BothOperatorsEnabled)

	// Validate exactly one ServiceMesh CR exists (singleton)
	tc.EnsureResourcesExist(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(HaveLen(1)),
		WithCustomErrorMsg("ServiceMesh CR was expected to be a singleton"),
	)

	// Validate no ServiceMesh FeatureTrackers exist
	tc.EnsureResourcesDoNotExist(
		WithMinimalObject(gvk.FeatureTracker, tc.NamespacedName),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.SelectorFromSet(k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
			}),
		}),
		WithCustomErrorMsg("Expected no ServiceMesh-related FeatureTracker resources to be present"),
	)

	// Validate no legacy FeatureTrackers exist
	ftNames := getServiceMeshFeatureTrackerNames(tc.AppsNamespace)
	for _, name := range ftNames {
		tc.EnsureResourceGone(WithMinimalObject(gvk.FeatureTracker, types.NamespacedName{Name: name}))
	}
}

// ValidateServiceMeshControlPlane ensures ServiceMeshControlPlane instance is created and ready.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshControlPlane(t *testing.T) {
	t.Helper()

	// Validate the default name, namespace, and Ready condition for SMCP
	conditions := append(
		[]gTypes.GomegaMatcher{jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)},
		withBasicMetadata(serviceMeshControlPlaneDefaultName, serviceMeshControlPlaneNamespace)...,
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMeshControlPlane, namespacedServiceMeshControlPlane),
		WithCondition(And(conditions...)),
	)

	// validate SMCP deployment
	smcpDeploymentName := fmt.Sprintf("istiod-%s", serviceMeshControlPlaneDefaultName)
	smcpDeploymentConditions := withBasicMetadata(smcpDeploymentName, serviceMeshControlPlaneNamespace)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      smcpDeploymentName,
			Namespace: serviceMeshControlPlaneNamespace,
		}),
		WithCondition(And(smcpDeploymentConditions...)),
	)
}

// ValidateAuthorinoResources ensures Authorino resource is ready and Authorino deployment template was properly annotated.
// Also checks ServiceMeshMember resource is created and annotated with correct control plane reference.
func (tc *ServiceMeshTestCtx) ValidateAuthorinoResources(t *testing.T) {
	t.Helper()

	// Validate Authorino resource
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Authorino, namespacedAuthorino),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
		)),
	)

	// Validate authorino deployment is labeled for sidecar injection
	deploymentCondition := jq.Match(`.spec.template.metadata.labels | has("sidecar.istio.io/inject") and .["sidecar.istio.io/inject"] == "true"`)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
		WithCondition(deploymentCondition),
	)

	// Validate authorino deployment is re-annotated after deletion
	tc.EnsureResourceDeletedThenRecreated(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
		WithCondition(deploymentCondition),
	)

	// validate auth ServiceMeshMember
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMeshMember, namespacedServiceMeshMember),
		WithCondition(And(append(append(
			[]gTypes.GomegaMatcher{jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)},
			withBasicMetadata(serviceMeshMemberDefaultName, authorinoDefaultNamespace)...),
			// check control plane reference
			jq.Match(`.spec.controlPlaneRef.name == "%s"`, serviceMeshControlPlaneDefaultName),
			jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, serviceMeshControlPlaneNamespace),
			// check owner reference
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
		)...)),
	)
}

// ValidateServiceMeshMetricsCollectionResources ensures ServiceMesh metrics collection resources are created.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshMetricsCollectionResources(t *testing.T) {
	t.Helper()

	// ServiceMonitor validation
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMonitorServiceMesh, namespacedServiceMonitor),
		WithCondition(And(append(
			withMetadataConditions(serviceMonitorDefaultName, serviceMeshControlPlaneNamespace, tc.GVK.Kind),
			// Spec configuration checks
			jq.Match(`.spec.targetLabels[0] == "app"`),
			jq.Match(`.spec.selector.matchLabels.istio == "pilot"`),
			jq.Match(`.spec.endpoints[0].port == "http-monitoring"`),
			jq.Match(`.spec.endpoints[0].interval == "30s"`),
		)...)),
	)

	// PodMonitor validation
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.PodMonitorServiceMesh, namespacedPodMonitor),
		WithCondition(And(append(
			withMetadataConditions(podMonitorDefaultName, serviceMeshControlPlaneNamespace, tc.GVK.Kind),
			// Spec configuration checks
			jq.Match(`.spec.selector.matchExpressions[0].key == "istio-prometheus-ignore"`),
			jq.Match(`.spec.selector.matchExpressions[0].operator == "DoesNotExist"`),
			jq.Match(`.spec.podMetricsEndpoints[0].path == "/stats/prometheus"`),
			jq.Match(`.spec.podMetricsEndpoints[0].port == "http-envoy-prom"`),
			jq.Match(`.spec.podMetricsEndpoints[0].scheme == "http"`),
			jq.Match(`.spec.podMetricsEndpoints[0].interval == "30s"`),
		)...)),
	)
}

// ValidateServiceMeshMetricsCollectionDisabled ensures ServiceMesh metrics collection disabling cleans up metrics collection resources.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshMetricsCollectionDisabled(t *testing.T) {
	t.Helper()

	// update DSCI to disable Metrics Collection
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.controlPlane.metricsCollection = "%s"`, "None")),
		WithCondition(
			And(
				append(
					dsciAvailableConditions,
					withCapabilityConditions(true, true)...,
				)...,
			),
		),
	)

	// Verify specific ServiceMesh metrics collection resources got cleaned up
	tc.ensureMonitoringResourcesGone()

	// ensure ServiceMesh CR instance remains unaffected and stays ready
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(And(serviceMeshReadyConditions...)),
	)

	tc.ensureDSCIReady()
}

// ValidateServiceMeshTransitionToUnmanaged ensures ServiceMesh CR is properly updated to Unmanaged state.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshTransitionToUnmanaged(t *testing.T) {
	t.Helper()

	// pre-test: setup default ServiceMesh environment
	tc.setupAndValidateServiceMeshEnvironment(t, BothOperatorsEnabled)

	// ensure DSCI is updated to set ServiceMesh as Unmanaged
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Unmanaged)),
		WithCondition(And(dsciAvailableConditions...)),
	)

	// ensure ServiceMesh instance does exist and is ready, without any ServiceMesh capabilities due to Unmanaged state
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			And(
				append(append(serviceMeshReadyConditions,
					// ensure managementState propagated to ServiceMesh instance
					jq.Match(`.spec.managementState == "%s"`, operatorv1.Unmanaged)),
					// check capabilities, should be false as ServiceMesh is Unmanaged
					withCapabilityConditions(false, false)...,
				)...,
			),
		),
	)

	// ensure DSCI instance also has appropriate status conditions
	// Available and ReconcileComplete as True, with ServiceMesh capabilities as False due to Unmanaged state
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(And(
			append(dsciAvailableConditions,
				// check capabilities, should be false as ServiceMesh is Unmanaged
				withCapabilityConditions(false, false)...,
			)...,
		)),
	)

	// post-test: restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, BothOperatorsEnabled)
}

// ValidateServiceMeshRemoved ensures Removed state is handled properly.
// ServiceMesh CR is expected to be removed along with all ServiceMesh-related resources.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshTransitionToRemoved(t *testing.T) {
	t.Helper()

	// pre-test: setup default ServiceMesh environment
	tc.setupAndValidateServiceMeshEnvironment(t, BothOperatorsEnabled)

	// ensure ServiceMesh CR is created and ready
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(And(serviceMeshReadyConditions...)),
	)

	// remove/cleanup ServiceMesh via setting ServiceMesh managementState to Removed in DSCI
	tc.cleanupServiceMeshConfiguration(t)

	// ensure DSCI is ready and has ServiceMesh capabilities as False due to Removed state
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(And(
			append(dsciAvailableConditions,
				withCapabilityConditions(false, false)...,
			)...,
		)),
	)

	// post-test:restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, BothOperatorsEnabled)
}

// ValidateLegacyServiceMeshFeatureTrackersRemoval ensures legacy FeatureTrackers are cleaned up during ServiceMesh installation.
func (tc *ServiceMeshTestCtx) ValidateLegacyServiceMeshFeatureTrackersRemoval(t *testing.T) {
	t.Helper()

	ftNames := getServiceMeshFeatureTrackerNames(tc.AppsNamespace)

	// remove ServiceMesh to provide ground for clean ServiceMesh installation
	tc.cleanupServiceMeshConfiguration(t)

	// create dummy legacy ServiceMesh-related FeatureTrackers
	tc.createDummyServiceMeshFeatureTrackers(t, ftNames)

	// install ServiceMesh with default config
	tc.setupAndValidateServiceMeshEnvironment(t, BothOperatorsEnabled)

	// ensure legacy ServiceMesh-related FeatureTrackers are gone after ServiceMesh installation
	for _, name := range ftNames {
		tc.EnsureResourceGone(WithMinimalObject(gvk.FeatureTracker, types.NamespacedName{Name: name}))
	}
}

// ValidateAuthorinoOperatorNotInstalled verifies ServiceMesh behavior when the Authorino operator is missing.
func (tc *ServiceMeshTestCtx) ValidateAuthorinoOperatorNotInstalled(t *testing.T) {
	t.Helper()

	// pre-test: cleanup ServiceMesh and its resources
	// to emulate starting conditions for the clean ServiceMesh installation
	tc.cleanupServiceMeshConfiguration(t)

	// attempt re-enabling ServiceMesh with Authorino operator not installed, and validate state
	tc.setupAndValidateServiceMeshEnvironment(t, OnlyServiceMeshEnabled)

	// ensure Authorino instance was not created
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Authorino, namespacedAuthorino),
	)

	// post-test:cleanup ServiceMesh and its resources again, for post-test recovery purposes
	tc.cleanupServiceMeshConfiguration(t)

	// post-test: restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, BothOperatorsEnabled)
}

// Helper functions for validation conditions.

// withCapabilityConditions creates ServiceMesh capability validation conditions based on operator installation status.
func withCapabilityConditions(serviceMeshEnabled, authorinoEnabled bool) []gTypes.GomegaMatcher {
	serviceMeshStatus := metav1.ConditionFalse
	authorinoStatus := metav1.ConditionFalse
	if serviceMeshEnabled {
		serviceMeshStatus = metav1.ConditionTrue
	}
	if authorinoEnabled {
		authorinoStatus = metav1.ConditionTrue
	}

	return []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.CapabilityServiceMesh, serviceMeshStatus),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.CapabilityServiceMeshAuthorization, authorinoStatus),
	}
}

// withMetadataConditions creates common metadata validation conditions for ServiceMesh-owned resources.
func withMetadataConditions(name, namespace, ownerKind string) []gTypes.GomegaMatcher {
	return []gTypes.GomegaMatcher{
		jq.Match(`.metadata.name == "%s"`, name),
		jq.Match(`.metadata.namespace == "%s"`, namespace),
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, ownerKind),
	}
}

// withBasicMetadata creates basic name and namespace validation conditions.
func withBasicMetadata(name, namespace string) []gTypes.GomegaMatcher {
	return []gTypes.GomegaMatcher{
		jq.Match(`.metadata.name == "%s"`, name),
		jq.Match(`.metadata.namespace == "%s"`, namespace),
	}
}

// setupAndValidateServiceMeshEnvironment configures operators and validates complete ServiceMesh environment setup.
func (tc *ServiceMeshTestCtx) setupAndValidateServiceMeshEnvironment(t *testing.T, dependentOperatorsConfig DependentOperatorsTestConfig) {
	t.Helper()

	// setup dependent operators according to the config
	tc.setupOperators(t, dependentOperatorsConfig)

	// update DSCI with default config for ServiceMesh
	tc.setupAndValidateDsciInstance(t, dependentOperatorsConfig)

	// validate ServiceMesh CR instance that gets created based on DSCI's spec
	tc.validateServiceMeshInstance(t, dependentOperatorsConfig)
}

// setupOperators installs or uninstalls ServiceMesh and Authorino operators based on configuration.
func (tc *ServiceMeshTestCtx) setupOperators(t *testing.T, dependentOperatorsConfig DependentOperatorsTestConfig) {
	t.Helper()

	// Define operators to manage with their installation flags
	operators := []struct {
		name          string
		shouldInstall bool
	}{
		{serviceMeshOpName, dependentOperatorsConfig.EnsureServiceMeshOperatorInstalled},
		{authorinoOpName, dependentOperatorsConfig.EnsureAuthorinoOperatorInstalled},
	}

	// Setup each operator based on configuration
	for _, op := range operators {
		operatorNS := types.NamespacedName{Name: op.name, Namespace: openshiftOperatorsNamespace}
		if op.shouldInstall {
			tc.EnsureOperatorInstalled(operatorNS, true)
		} else {
			tc.UninstallOperator(operatorNS, WithWaitForDeletion(true))
		}
	}
}

// setupAndValidateDsciInstance configures DSCI with ServiceMesh settings and validates expected conditions.
func (tc *ServiceMeshTestCtx) setupAndValidateDsciInstance(t *testing.T, dependentOperatorsConfig DependentOperatorsTestConfig) {
	t.Helper()

	// ServiceMesh capability condition
	serviceMeshStatus := metav1.ConditionFalse
	if dependentOperatorsConfig.EnsureServiceMeshOperatorInstalled {
		serviceMeshStatus = metav1.ConditionTrue
	}

	// Authorino capability condition
	authorinoStatus := metav1.ConditionFalse
	if dependentOperatorsConfig.EnsureAuthorinoOperatorInstalled {
		authorinoStatus = metav1.ConditionTrue
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Managed),
				testf.Transform(`.spec.serviceMesh.controlPlane.metricsCollection = "%s"`, serviceMeshMetricsCollectionDefault),
				testf.Transform(`.spec.serviceMesh.controlPlane.name = "%s"`, serviceMeshControlPlaneDefaultName),
				testf.Transform(`.spec.serviceMesh.controlPlane.namespace = "%s"`, serviceMeshControlPlaneNamespace),
			),
		),
		WithCondition(
			And(
				append(
					dsciAvailableConditions,
					// ServiceMesh spec validation
					jq.Match(`.spec.serviceMesh.managementState == "%s"`, operatorv1.Managed),
					jq.Match(`.spec.serviceMesh.controlPlane.metricsCollection == "%s"`, serviceMeshMetricsCollectionDefault),
					jq.Match(`.spec.serviceMesh.controlPlane.name == "%s"`, serviceMeshControlPlaneDefaultName),
					jq.Match(`.spec.serviceMesh.controlPlane.namespace == "%s"`, serviceMeshControlPlaneNamespace),
					// ServiceMesh and Authorino capability conditions
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.CapabilityServiceMesh, serviceMeshStatus),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.CapabilityServiceMeshAuthorization, authorinoStatus),
				)...,
			),
		),
	)
}

// validateServiceMeshInstance verifies ServiceMesh CR creation with correct specifications and readiness conditions.
func (tc *ServiceMeshTestCtx) validateServiceMeshInstance(t *testing.T, dependentOperatorsConfig DependentOperatorsTestConfig) {
	t.Helper()

	// ensure ServiceMesh instance was automatically created with valid config
	serviceMeshExpectedConditions := []gTypes.GomegaMatcher{
		// ensure spec matches DSCI's spec.serviceMesh
		jq.Match(`.spec.managementState == "%s"`, operatorv1.Managed),
		jq.Match(`.spec.controlPlane.metricsCollection == "%s"`, serviceMeshMetricsCollectionDefault),
		jq.Match(`.spec.controlPlane.name == "%s"`, serviceMeshControlPlaneDefaultName),
		jq.Match(`.spec.controlPlane.namespace == "%s"`, serviceMeshControlPlaneNamespace),
		// ensure default auth audiences are set
		jq.Match(`.spec.auth.audiences[0] == "%s"`, defaultAuthAudience),
		// ensure DSCI is the owner of ServiceMesh instance
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DSCInitialization.Kind),
		// add checks for readiness conditions expected as True
	}

	// Add Ready/ProvisioningSucceeded conditions
	readyStatus := metav1.ConditionFalse
	provisioningStatus := metav1.ConditionFalse
	serviceMeshCapabilityStatus := metav1.ConditionFalse
	if dependentOperatorsConfig.EnsureServiceMeshOperatorInstalled {
		readyStatus = metav1.ConditionTrue
		provisioningStatus = metav1.ConditionTrue
		serviceMeshCapabilityStatus = metav1.ConditionTrue
	}

	// Add Authorino capability condition
	authorinoStatus := metav1.ConditionFalse
	if dependentOperatorsConfig.EnsureAuthorinoOperatorInstalled {
		authorinoStatus = metav1.ConditionTrue
	}

	// Add all status conditions
	serviceMeshExpectedConditions = append(serviceMeshExpectedConditions,
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, readyStatus),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, provisioningStatus),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.CapabilityServiceMesh, serviceMeshCapabilityStatus),
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.CapabilityServiceMeshAuthorization, authorinoStatus),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(And(serviceMeshExpectedConditions...)),
		WithCustomErrorMsg("ServiceMesh instance was expected to be created with default config from DSCI, and ready conditions as True"),
	)
}

// ensureDSCIReady validates that DSCI is available and reconciliation is complete.
func (tc *ServiceMeshTestCtx) ensureDSCIReady() {
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(And(dsciAvailableConditions...)),
	)
}

// ensureServiceMeshGone removes ServiceMesh CR and all associated resources.
func (tc *ServiceMeshTestCtx) ensureServiceMeshGone(t *testing.T) {
	t.Helper()

	// ensure ServiceMesh owned resources are deleted
	tc.ensureServiceMeshResourcesGone(t)

	// ensure ServiceMesh CR instance itself is deleted
	tc.EnsureResourceGone(WithMinimalObject(tc.GVK, tc.NamespacedName))
}

// ensureServiceMeshResourcesGone verifies all ServiceMesh-related resources are deleted.
func (tc *ServiceMeshTestCtx) ensureServiceMeshResourcesGone(t *testing.T) {
	t.Helper()

	tc.EnsureResourceGone(WithMinimalObject(gvk.ServiceMeshControlPlane, namespacedServiceMeshControlPlane))
	tc.EnsureResourceGone(WithMinimalObject(gvk.ServiceMeshMember, namespacedServiceMeshMember))
	tc.EnsureResourceGone(WithMinimalObject(gvk.Authorino, namespacedAuthorino))
	tc.ensureMonitoringResourcesGone()
}

// ensureMonitoringResourcesGone removes ServiceMesh monitoring resources (ServiceMonitor and PodMonitor).
func (tc *ServiceMeshTestCtx) ensureMonitoringResourcesGone() {
	tc.EnsureResourceGone(WithMinimalObject(gvk.ServiceMonitorServiceMesh, namespacedServiceMonitor))
	tc.EnsureResourceGone(WithMinimalObject(gvk.PodMonitorServiceMesh, namespacedPodMonitor))
}

// cleanupServiceMeshConfiguration efficiently removes ServiceMesh configuration and resources.
func (tc *ServiceMeshTestCtx) cleanupServiceMeshConfiguration(t *testing.T) {
	t.Helper()

	// Set DSCI ServiceMesh to Removed state and clean up resources in one operation
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Removed)),
	)

	// ensure ServiceMesh owned resources do not exist
	tc.ensureServiceMeshGone(t)
}

// createDummyServiceMeshFeatureTrackers creates test FeatureTracker resources for legacy cleanup testing.
func (tc *ServiceMeshTestCtx) createDummyServiceMeshFeatureTrackers(t *testing.T, ftNames []string) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	for _, name := range ftNames {
		ft := &featuresv1.FeatureTracker{}
		ft.SetName(name)

		tc.EventuallyResourceCreatedOrUpdated(
			WithMinimalObject(gvk.FeatureTracker, types.NamespacedName{Name: name}),
			WithMutateFunc(func(obj *unstructured.Unstructured) error {
				if err := controllerutil.SetOwnerReference(dsci, obj, tc.Client().Scheme()); err != nil {
					return err
				}

				// Trigger reconciliation as spec changes.
				if err := unstructured.SetNestedField(obj.Object, xid.New().String(), "spec", "source", "name"); err != nil {
					return err
				}

				return nil
			}),
			WithCustomErrorMsg("error creating or updating pre-existing FeatureTracker"),
		)
	}
}

// getServiceMeshFeatureTrackerNames returns the list of legacy ServiceMesh FeatureTracker names for testing.
func getServiceMeshFeatureTrackerNames(appsNamespace string) []string {
	return []string{
		appsNamespace + "-mesh-shared-configmap",
		appsNamespace + "-mesh-control-plane-creation",
		appsNamespace + "-mesh-metrics-collection",
		appsNamespace + "-enable-proxy-injection-in-authorino-deployment",
		appsNamespace + "-mesh-control-plane-external-authz",
	}
}
