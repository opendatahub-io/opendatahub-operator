package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// ServiceMesh constants.
const (
	serviceMeshControlPlaneDefaultName = "data-science-smcp"
	serviceMeshControlPlaneNamespace   = "istio-system"
)

var (
	namespacedServiceMeshControlPlane = types.NamespacedName{
		Name:      serviceMeshControlPlaneDefaultName,
		Namespace: serviceMeshControlPlaneNamespace,
	}
)

// Authorino constants.
const (
	authorinoDefaultName         = "authorino"
	authorinoDefaultNamespace    = "redhat-ods-applications-auth-provider"
	defaultAuthAudience          = "https://kubernetes.default.svc"
	serviceMeshMemberDefaultName = "default"
)

var (
	namespacedServiceMeshMember = types.NamespacedName{
		Name:      serviceMeshMemberDefaultName,
		Namespace: authorinoDefaultNamespace,
	}
	namespacedAuthorino = types.NamespacedName{
		Name:      authorinoDefaultName,
		Namespace: authorinoDefaultNamespace,
	}
)

// ServiceMesh metrics collection constants.
const (
	serviceMeshMetricsCollectionDefault = "Istio"
)

var (
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
)

var (
	dsciAvailableConditions = []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "%s"`, metav1.ConditionTrue),
		jq.Match(`.status.conditions[] | select(.type == "ReconcileComplete") | .status == "%s"`, metav1.ConditionTrue),
	}
)

const (
	serviceMeshOperatorDefaultChannel = "stable"
	authorinoOperatorDefaultChannel   = "stable"
)

func getServiceMeshFeatureTrackerNames(appsNamespace string) []string {
	return []string{
		appsNamespace + "-mesh-shared-configmap",
		appsNamespace + "-mesh-control-plane-creation",
		appsNamespace + "-mesh-metrics-collection",
		appsNamespace + "-enable-proxy-injection-in-authorino-deployment",
		appsNamespace + "-mesh-control-plane-external-authz",
	}
}

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
		{"Validate default ServiceMesh configuration", smCtx.ValidateDefaultServiceMeshCreation},
		{"Validate ServiceMesh is singleton", smCtx.ValidateServiceMeshIsSingleton},
		{"Validate No ServiceMesh FeatureTrackers exist", smCtx.ValidateNoServiceMeshFeatureTrackersExist},
		{"Validate No Legacy FeatureTrackers exist", smCtx.ValidateNoLegacyFeatureTrackersExist},
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
		{"Validate ServiceMesh operator not installed", smCtx.ValidateServiceMeshOperatorNotInstalled},
		{"Validate Authorino operator not installed", smCtx.ValidateAuthorinoOperatorNotInstalled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *ServiceMeshTestCtx) ValidateDefaultServiceMeshCreation(t *testing.T) {
	t.Helper()

	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})
}

func (tc *ServiceMeshTestCtx) ValidateServiceMeshIsSingleton(t *testing.T) {
	t.Helper()

	// ensure that exactly one ServiceMesh CR exists
	tc.EnsureResourcesExist(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			And(
				HaveLen(1),
			),
		),
		WithCustomErrorMsg("ServiceMesh CR was expected to be a singleton"),
	)
}

// ValidateNoServiceMeshFeatureTrackers ensures there are no FeatureTrackers for ServiceMesh present in the cluster.
func (tc *ServiceMeshTestCtx) ValidateNoServiceMeshFeatureTrackersExist(t *testing.T) {
	t.Helper()

	tc.EnsureResourcesDoNotExist(
		WithMinimalObject(gvk.FeatureTracker, tc.NamespacedName),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.SelectorFromSet(
				k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				},
			),
		}),
		WithCustomErrorMsg("Expected no ServiceMesh-related FeatureTracker resources to be present"),
	)
}

func (tc *ServiceMeshTestCtx) ValidateNoLegacyFeatureTrackersExist(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	ftNames := getServiceMeshFeatureTrackerNames(dsci.Spec.ApplicationsNamespace)

	for _, name := range ftNames {
		tc.EnsureResourceGone(WithMinimalObject(gvk.FeatureTracker, types.NamespacedName{Name: name}))
	}
}

// ValidateServiceMeshControlPlane ensures ServiceMeshControlPlane instance is created and ready.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshControlPlane(t *testing.T) {
	t.Helper()

	// Validate the default name, namespace, and Ready condition for SMCP
	conditions := []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
		jq.Match(`.metadata.name == "%s"`, serviceMeshControlPlaneDefaultName),
		jq.Match(`.metadata.namespace == "%s"`, serviceMeshControlPlaneNamespace),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMeshControlPlane, namespacedServiceMeshControlPlane),
		WithCondition(And(conditions...)),
	)

	// validate SMCP deployment
	smcpDeploymentName := fmt.Sprintf("istiod-%s", serviceMeshControlPlaneDefaultName)
	smcpDeploymentConditions := []gTypes.GomegaMatcher{
		jq.Match(`.metadata.name == "%s"`, smcpDeploymentName),
		jq.Match(`.metadata.namespace == "%s"`, serviceMeshControlPlaneNamespace),
	}
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

	// Validate the "Ready" condition for authorino resource
	conditions := []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
		// also check owner reference
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Authorino, namespacedAuthorino),
		WithCondition(And(conditions...)),
	)

	// Validate authorino deployment was annotated
	conditionsDeployment := []gTypes.GomegaMatcher{
		jq.Match(`.spec.template.metadata.labels | has("sidecar.istio.io/inject") and .["sidecar.istio.io/inject"] == "true"`),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
		WithCondition(And(conditionsDeployment...)),
	)

	// Validate authorino deployment is re-annotated after deletion
	tc.DeleteResource(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
		WithCondition(And(conditionsDeployment...)),
	)

	// validate auth ServiceMeshMember
	smmConditions := []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
		// check name and namespace
		jq.Match(`.metadata.name == "%s"`, serviceMeshMemberDefaultName),
		jq.Match(`.metadata.namespace == "%s"`, authorinoDefaultNamespace),
		// check control plane reference
		jq.Match(`.spec.controlPlaneRef.name == "%s"`, serviceMeshControlPlaneDefaultName),
		jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, serviceMeshControlPlaneNamespace),
		// check owner reference
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMeshMember, namespacedServiceMeshMember),
		WithCondition(And(smmConditions...)),
	)
}

// ValidateServiceMeshMetricsCollectionResources ensures ServiceMesh metrics collection resources are created.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshMetricsCollectionResources(t *testing.T) {
	t.Helper()

	// validate ServiceMonitor
	serviceMonitorConditions := []gTypes.GomegaMatcher{
		jq.Match(`.metadata.name == "%s"`, serviceMonitorDefaultName),
		jq.Match(`.metadata.namespace == "%s"`, serviceMeshControlPlaneNamespace),
		// check owner reference
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
		// check spec contents
		jq.Match(`.spec.targetLabels[0] == "app"`),
		jq.Match(`.spec.selector.matchLabels.istio == "pilot"`),
		jq.Match(`.spec.endpoints[0].port == "http-monitoring"`),
		jq.Match(`.spec.endpoints[0].interval == "30s"`),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMonitorServiceMesh, namespacedServiceMonitor),
		WithCondition(And(serviceMonitorConditions...)),
	)

	// validate PodMonitor
	podMonitorConditions := []gTypes.GomegaMatcher{
		jq.Match(`.metadata.name == "%s"`, podMonitorDefaultName),
		jq.Match(`.metadata.namespace == "%s"`, serviceMeshControlPlaneNamespace),
		// check owner reference
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
		// check spec contents
		jq.Match(`.spec.selector.matchExpressions[0].key == "istio-prometheus-ignore"`),
		jq.Match(`.spec.selector.matchExpressions[0].operator == "DoesNotExist"`),
		jq.Match(`.spec.podMetricsEndpoints[0].path == "/stats/prometheus"`),
		jq.Match(`.spec.podMetricsEndpoints[0].port == "http-envoy-prom"`),
		jq.Match(`.spec.podMetricsEndpoints[0].scheme == "http"`),
		jq.Match(`.spec.podMetricsEndpoints[0].interval == "30s"`),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.PodMonitorServiceMesh, namespacedPodMonitor),
		WithCondition(And(podMonitorConditions...)),
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
					jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionTrue),
				)...,
			),
		),
	)

	// Verify specific ServiceMesh metrics collection resources got cleaned up
	tc.EnsureResourceGone(WithMinimalObject(gvk.ServiceMonitorServiceMesh, namespacedServiceMonitor))
	tc.EnsureResourceGone(WithMinimalObject(gvk.PodMonitorServiceMesh, namespacedPodMonitor))

	// ensure ServiceMesh CR instance remains unaffected and stays ready
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "%s"`, metav1.ConditionTrue),
		)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(And(dsciAvailableConditions...)),
	)
}

// ValidateServiceMeshTransitionToUnmanaged ensures ServiceMesh CR is properly updated to Unmanaged state.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshTransitionToUnmanaged(t *testing.T) {
	t.Helper()

	// pre-test: setup default ServiceMesh environment
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})

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
				jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "%s"`, metav1.ConditionTrue),
				// ensure managementState propagated to ServiceMesh instance
				jq.Match(`.spec.managementState == "%s"`, operatorv1.Unmanaged),
				// check capabilities, should be false as ServiceMesh is Unmanaged
				jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionFalse),
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
				jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionFalse),
			)...,
		)),
	)

	// post-test: restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})
}

// ValidateServiceMeshRemoved ensures Removed state is handled properly.
// ServiceMesh CR is expected to be removed along with all ServiceMesh-related resources.
func (tc *ServiceMeshTestCtx) ValidateServiceMeshTransitionToRemoved(t *testing.T) {
	t.Helper()

	// pre-test: setup default ServiceMesh environment
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})

	// ensure ServiceMesh CR is created and ready
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "%s"`, metav1.ConditionTrue),
			),
		),
	)

	// remove/cleanup ServiceMesh via setting ServiceMesh managementState to Removed in DSCI
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Removed)),
	)
	tc.ensureServiceMeshGone(t)

	// ensure DSCI is ready and has ServiceMesh capabilities as False due to Removed state
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(And(
			append(dsciAvailableConditions,
				jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionFalse),
			)...,
		)),
	)

	// post-test:restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})
}

func (tc *ServiceMeshTestCtx) ValidateLegacyServiceMeshFeatureTrackersRemoval(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	ftNames := getServiceMeshFeatureTrackerNames(dsci.Spec.ApplicationsNamespace)

	// remove ServiceMesh to provide ground for clean ServiceMesh installation
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Removed)),
	)
	tc.ensureServiceMeshGone(t)

	// create dummy legacy ServiceMesh-related FeatureTrackers
	tc.createDummyServiceMeshFeatureTrackers(t, ftNames)

	// install ServiceMesh with default config
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})

	// ensure legacy ServiceMesh-related FeatureTrackers are gone after ServiceMesh installation
	for _, name := range ftNames {
		tc.EnsureResourceGone(WithMinimalObject(gvk.FeatureTracker, types.NamespacedName{Name: name}))
	}
}

func (tc *ServiceMeshTestCtx) ValidateNoServiceMeshSpecInDSCI(t *testing.T) {
	t.Helper()

	// remove ServiceMesh spec from DSCI
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`del(.spec.serviceMesh)`)),
		WithCondition(
			And(
				append(dsciAvailableConditions,
					jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionFalse),
				)...,
			),
		),
	)

	// ensure ServiceMesh owned resources do not exist
	// also ensure ServicesMeshControlPlane does not exist (via ServiceMesh finalizer, as it's not owned by ServiceMesh CR)
	tc.ensureServiceMeshResourcesGone(t)
	// ensure ServiceMesh CR instance itself is deleted
	tc.EnsureResourceGone(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
	)

	// ensure DSCI remains available
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(And(dsciAvailableConditions...)),
	)

	// post-test:restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})
}

func (tc *ServiceMeshTestCtx) ValidateServiceMeshOperatorNotInstalled(t *testing.T) {
	t.Helper()

	// pre-test: cleanup ServiceMesh and its resources
	// to emulate starting conditions for the clean ServiceMesh installation
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Removed)),
	)
	tc.ensureServiceMeshGone(t)

	// attempt installing ServiceMesh with missing ServiceMesh operator, and validate state
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: false,
		EnsureAuthorinoOperatorInstalled:   false,
	})

	// ensure ServiceMesh resources were not created
	tc.ensureServiceMeshResourcesGone(t)

	// post-test:cleanup ServiceMesh and its resources again, for post-test recovery purposes
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Removed)),
	)
	tc.ensureServiceMeshGone(t)
	// post-test: restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})
}

func (tc *ServiceMeshTestCtx) ValidateAuthorinoOperatorNotInstalled(t *testing.T) {
	t.Helper()

	// pre-test: cleanup ServiceMesh and its resources
	// to emulate starting conditions for the clean ServiceMesh installation
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Removed)),
	)
	tc.ensureServiceMeshGone(t)

	// attempt re-enabling ServiceMesh with Authorino operator not installed, and validate state
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   false,
	})

	// ensure Authorino instance was not created
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Authorino, namespacedAuthorino),
	)

	// post-test:cleanup ServiceMesh and its resources again, for post-test recovery purposes
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.serviceMesh.managementState = "%s"`, operatorv1.Removed)),
	)
	tc.ensureServiceMeshGone(t)
	// post-test: restore DSCI ServiceMesh spec to default config
	tc.setupAndValidateServiceMeshEnvironment(t, DependentOperatorsTestConfig{
		EnsureServiceMeshOperatorInstalled: true,
		EnsureAuthorinoOperatorInstalled:   true,
	})
}

func (tc *ServiceMeshTestCtx) setupAndValidateServiceMeshEnvironment(t *testing.T, dependentOperatorsConfig DependentOperatorsTestConfig) {
	t.Helper()

	// setup dependent operators according to the config
	tc.setupOperators(t, dependentOperatorsConfig)

	// update DSCI with default config for ServiceMesh
	tc.setupAndValidateDsciInstance(t, dependentOperatorsConfig)

	// validate ServiceMesh CR instance that gets created based on DSCI's spec
	tc.validateServiceMeshInstance(t, dependentOperatorsConfig)
}

func (tc *ServiceMeshTestCtx) setupOperators(t *testing.T, dependentOperatorsConfig DependentOperatorsTestConfig) {
	t.Helper()

	tc.setupServiceMeshOperator(t, dependentOperatorsConfig.EnsureServiceMeshOperatorInstalled)
	tc.setupAuthorinoOperator(t, dependentOperatorsConfig.EnsureAuthorinoOperatorInstalled)
}

func (tc *ServiceMeshTestCtx) setupServiceMeshOperator(t *testing.T, shouldBeInstalled bool) {
	t.Helper()

	if shouldBeInstalled {
		tc.EnsureOperatorInstalled(types.NamespacedName{Name: serviceMeshOpName, Namespace: openshiftOperatorsNamespace}, true)
	} else {
		tc.uninstallOperatorWithChannel(
			t,
			types.NamespacedName{Name: serviceMeshOpName, Namespace: openshiftOperatorsNamespace},
			serviceMeshOperatorDefaultChannel,
		)
		time.Sleep(5 * time.Second)
	}
}

func (tc *ServiceMeshTestCtx) setupAuthorinoOperator(t *testing.T, shouldBeInstalled bool) {
	t.Helper()

	if shouldBeInstalled {
		tc.EnsureOperatorInstalled(types.NamespacedName{Name: authorinoOpName, Namespace: openshiftOperatorsNamespace}, true)
	} else {
		tc.uninstallOperatorWithChannel(
			t,
			types.NamespacedName{Name: authorinoOpName, Namespace: openshiftOperatorsNamespace},
			authorinoOperatorDefaultChannel,
		)
		time.Sleep(5 * time.Second)
	}
}

func (tc *ServiceMeshTestCtx) setupAndValidateDsciInstance(t *testing.T, dependentOperatorsConfig DependentOperatorsTestConfig) {
	t.Helper()

	// ensure DSCI is created with valid config for ServiceMesh
	dsciExpectedConditions := []gTypes.GomegaMatcher{}
	dsciExpectedConditions = append(dsciExpectedConditions, dsciAvailableConditions...)
	dsciExpectedConditions = append(dsciExpectedConditions,
		jq.Match(`.spec.serviceMesh.managementState == "%s"`, operatorv1.Managed),
		jq.Match(`.spec.serviceMesh.controlPlane.metricsCollection == "%s"`, serviceMeshMetricsCollectionDefault),
		jq.Match(`.spec.serviceMesh.controlPlane.name == "%s"`, serviceMeshControlPlaneDefaultName),
		jq.Match(`.spec.serviceMesh.controlPlane.namespace == "%s"`, serviceMeshControlPlaneNamespace),
	)

	if dependentOperatorsConfig.EnsureServiceMeshOperatorInstalled {
		dsciExpectedConditions = append(dsciExpectedConditions,
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionTrue),
		)
	} else {
		dsciExpectedConditions = append(dsciExpectedConditions,
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionFalse),
		)
	}

	if dependentOperatorsConfig.EnsureAuthorinoOperatorInstalled {
		dsciExpectedConditions = append(dsciExpectedConditions,
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionTrue),
		)
	} else {
		dsciExpectedConditions = append(dsciExpectedConditions,
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionFalse),
		)
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
		WithCondition(And(dsciExpectedConditions...)),
	)
}

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

	if dependentOperatorsConfig.EnsureServiceMeshOperatorInstalled {
		serviceMeshExpectedConditions = append(
			serviceMeshExpectedConditions,
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "%s"`, metav1.ConditionTrue),
			// add check for ServiceMesh capability expected as True
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionTrue),
		)
	} else {
		serviceMeshExpectedConditions = append(
			serviceMeshExpectedConditions,
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "%s"`, metav1.ConditionFalse),
			// add check for ServiceMesh capability expected as False
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMesh") | .status == "%s"`, metav1.ConditionFalse),
		)
	}

	if dependentOperatorsConfig.EnsureAuthorinoOperatorInstalled {
		serviceMeshExpectedConditions = append(
			serviceMeshExpectedConditions,
			// ensure ServiceMesh Authorization capability is True
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionTrue),
		)
	} else {
		serviceMeshExpectedConditions = append(
			serviceMeshExpectedConditions,
			jq.Match(`.status.conditions[] | select(.type == "CapabilityServiceMeshAuthorization") | .status == "%s"`, metav1.ConditionFalse),
		)
	}

	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(And(serviceMeshExpectedConditions...)),
		WithCustomErrorMsg("ServiceMesh instance was expected to be created with default config from DSCI, and ready conditions as True"),
	)
}

// uninstallOperatorWithChannel delete an operator install subscription to a specific channel if exists.
func (tc *ServiceMeshTestCtx) uninstallOperatorWithChannel(t *testing.T, operatorNamespacedName types.NamespacedName, channel string) { //nolint:thelper,unparam
	// Check if operator subscription exists
	ro := tc.NewResourceOptions(WithMinimalObject(gvk.Subscription, operatorNamespacedName))
	operatorSubscription, err := tc.ensureResourceExistsOrNil(ro)

	if err != nil {
		t.Logf("Error checking if operator %s exists: %v", operatorNamespacedName.Name, err)
		return
	}

	if operatorSubscription != nil {
		t.Logf("Uninstalling %s operator", operatorNamespacedName.Name)

		csv, found, err := unstructured.NestedString(operatorSubscription.UnstructuredContent(), "status", "currentCSV")
		if !found || err != nil {
			t.Logf(".status.currentCSV expected to be present: %s with no error, Error: %v, but it wasn't. Deleting just the Subscription: %v", csv, err, operatorSubscription)
			tc.DeleteResource(WithMinimalObject(gvk.Subscription, operatorNamespacedName))
		} else {
			t.Logf("Deleting subscription %v and cluster service version %v", operatorNamespacedName, types.NamespacedName{Name: csv, Namespace: operatorSubscription.GetNamespace()})
			tc.DeleteResource(WithMinimalObject(gvk.Subscription, operatorNamespacedName))
			tc.DeleteResource(WithMinimalObject(gvk.ClusterServiceVersion, types.NamespacedName{Name: csv, Namespace: operatorSubscription.GetNamespace()}))
		}
	}

	t.Log("Operator uninstalled, proceeding")
}

func (tc *ServiceMeshTestCtx) ensureServiceMeshGone(t *testing.T) {
	t.Helper()

	// ensure ServiceMesh owned resources are deleted
	// also ensure ServicesMeshControlPlane is deleted (via ServiceMesh finalizer, as it's not owned by ServiceMesh CR)
	tc.ensureServiceMeshResourcesGone(t)

	// ensure ServiceMesh CR instance itself is deleted
	tc.EnsureResourceGone(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
	)
}

func (tc *ServiceMeshTestCtx) ensureServiceMeshResourcesGone(t *testing.T) {
	t.Helper()

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.ServiceMeshControlPlane, namespacedServiceMeshControlPlane),
	)
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.ServiceMeshControlPlane, namespacedServiceMeshControlPlane),
	)
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.ServiceMeshMember, namespacedServiceMeshMember),
	)
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Authorino, namespacedAuthorino),
	)
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.ServiceMonitorServiceMesh, namespacedServiceMonitor),
	)
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.PodMonitorServiceMesh, namespacedPodMonitor),
	)
}

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
