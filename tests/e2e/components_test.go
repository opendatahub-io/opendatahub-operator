package e2e_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// ComponentTestCtx holds the context for component tests.
type ComponentTestCtx struct {
	*TestContext

	// Any additional fields specific to component tests
	GVK            schema.GroupVersionKind
	NamespacedName types.NamespacedName

	// Subcomponent information (optional, only set for subcomponents)
	ParentKind            string // Kind of the parent component (e.g., "Kserve")
	SubComponentFieldName string // JSON field name of the subcomponent in parent's spec (e.g., "modelsAsService")
}

// CRD represents a custom resource definition with a name and version.
type CRD struct {
	Name    string
	Version string
}

// ControllerDiagnostics captures complete controller state for a component.
type ControllerDiagnostics struct {
	// Component State
	Generation         int64
	ObservedGeneration int64
	ResourceVersion    string
	GenerationGap      int64 // generation - observedGeneration

	// Conditions
	Conditions             []ConditionDiagnostic
	MostRecentConditionAge time.Duration
	OldestConditionAge     time.Duration

	// Controller Activity Verification
	TriggeredReconciliation bool
	ReconciliationLatency   time.Duration // How long to respond to triggered update
	ReconciliationSuccess   bool

	// Owner/Dependency Info
	OwnerReferences []OwnerRefDiagnostic
	Finalizers      []string
	Labels          map[string]string
	Annotations     map[string]string

	// Parent Component (for subcomponents)
	ParentExists          bool
	ParentManagementState string
	ParentGeneration      int64
	ParentObservedGen     int64
	ParentConditions      []ConditionDiagnostic

	// Operator Health
	OperatorPodCount    int
	OperatorPodsRunning int
	OperatorPodStatuses []PodStatusDiagnostic
	OperatorLeader      string // Which pod is leader

	// Assessment
	IsHealthy      bool
	Issues         []string // List of detected issues
	Recommendation string   // What to do (proceed, fail-fast, retry)
}

// ConditionDiagnostic captures detailed condition information.
type ConditionDiagnostic struct {
	Type               string
	Status             string
	Reason             string
	Message            string
	LastTransitionTime time.Time
	ObservedGeneration int64
	Age                time.Duration
}

// OwnerRefDiagnostic captures owner reference information.
type OwnerRefDiagnostic struct {
	APIVersion         string
	Kind               string
	Name               string
	UID                string
	Controller         bool
	BlockOwnerDeletion bool
}

// PodStatusDiagnostic captures pod health information.
type PodStatusDiagnostic struct {
	Name     string
	Phase    string
	Ready    bool
	Restarts int32
	Age      time.Duration
	IsLeader bool
}

// NewComponentTestCtx initializes a new component test context.
func NewComponentTestCtx(t *testing.T, object common.PlatformObject) (*ComponentTestCtx, error) { //nolint:thelper
	baseCtx, err := NewTestContext(t)
	if err != nil {
		return nil, err
	}

	ogvk, err := resources.GetGroupVersionKindForObject(baseCtx.Scheme(), object)
	if err != nil {
		return nil, err
	}

	componentCtx := ComponentTestCtx{
		TestContext:    baseCtx,
		GVK:            ogvk,
		NamespacedName: resources.NamespacedNameFromObject(object),
	}

	// Validate that the component CRD exists before running tests
	// This catches configuration errors early instead of waiting for long timeouts
	if err := ValidateComponentCRDExists(baseCtx, ogvk.Kind); err != nil {
		return nil, fmt.Errorf("component CRD validation failed: %w", err)
	}

	return &componentCtx, nil
}

// NewSubComponentTestCtx initializes a new component test context for a subcomponent.
// parentKind is the kind of the parent component (e.g., "Kserve").
// subComponentFieldName is the JSON field name of the subcomponent in the parent's spec (e.g., "modelsAsService").
func NewSubComponentTestCtx(t *testing.T, object common.PlatformObject, parentKind string, subComponentFieldName string) (*ComponentTestCtx, error) { //nolint:thelper
	componentCtx, err := NewComponentTestCtx(t, object)
	if err != nil {
		return nil, err
	}

	componentCtx.ParentKind = parentKind
	componentCtx.SubComponentFieldName = subComponentFieldName

	// For subcomponents, validate the parent component's CRD exists
	// (The subcomponent itself may not have a CRD, but the parent component should)
	if err := ValidateComponentCRDExists(componentCtx.TestContext, parentKind); err != nil {
		return nil, fmt.Errorf("parent component CRD validation failed for subcomponent %s: %w", componentCtx.GVK.Kind, err)
	}

	return componentCtx, nil
}

// ValidateComponentEnabled ensures that the component is enabled and its status is "Ready".
func (tc *ComponentTestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	// Ensure that DataScienceCluster exists and its component state is "Managed", with the "Ready" condition true.
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Managed)

	// Ensure the component resource exists and is marked "Ready".
	// Note: Ready=True already implies deployments exist and are ready (checked by DeploymentsAvailable condition)
	tc.EnsureResourcesExist(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			And(
				HaveLen(1),
				HaveEach(And(
					jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
				)),
			),
		),
	)
}

// ValidateComponentDisabled ensures that the component is disabled and its resources are deleted.
func (tc *ComponentTestCtx) ValidateComponentDisabled(t *testing.T) {
	t.Helper()

	// Ensure that the resources associated with the component exist
	tc.EnsureResourcesExist(WithMinimalObject(tc.GVK, tc.NamespacedName))

	// Ensure that DataScienceCluster exists and its component state is "Removed", with the "Ready" condition false.
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// Ensure that any Deployment resources for the component are not present
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	// Ensure that the resources associated with the component do not exist
	tc.EnsureResourcesGone(WithMinimalObject(tc.GVK, tc.NamespacedName))
}

// ValidateOperandsOwnerReferences ensures that all deployment resources have the correct owner references.
func (tc *ComponentTestCtx) ValidateOperandsOwnerReferences(t *testing.T) {
	t.Helper()

	// Ensure that the Deployment resources exist with the proper owner references
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithCondition(
			HaveEach(
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
			),
		),
		WithCustomErrorMsg("Deployment resources with correct owner references should exist"),
	)
}

func (tc *ComponentTestCtx) ValidateS3SecretCheckBucketExist(t *testing.T) {
	t.Helper()

	// Ensure the component is actually enabled before checking for the VAP
	// This handles cases where the component might have been temporarily disabled
	// by other test suites (e.g., ModelController, TrustyAI) and needs time to reconcile
	tc.ValidateComponentEnabled(t)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ValidatingAdmissionPolicy, types.NamespacedName{Name: "connectionapi-check-s3-bucket"}),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ValidatingAdmissionPolicyBinding, types.NamespacedName{Name: "connectionapi-check-s3-bucket-binding"}),
	)
}

// ValidateUpdateDeploymentsResources verifies the update of deployment replicas for the component.
func (tc *ComponentTestCtx) ValidateUpdateDeploymentsResources(t *testing.T) {
	t.Helper()

	// Ensure that deployments exist for the component
	deployments := tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
	)

	for _, d := range deployments {
		t.Run("deployment_"+d.GetName(), func(t *testing.T) {
			t.Helper()

			// Extract the current replica count
			replicas := ExtractAndExpectValue[int](tc.g, d, `.spec.replicas`, Not(BeNil()))

			expectedReplica := replicas + 1
			if replicas > 1 {
				expectedReplica = 1
			}

			// Update the deployment's replica count and wait for pods to be ready
			tc.EventuallyResourceCreatedOrUpdated(
				WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(&d)),
				WithMutateFunc(testf.Transform(`.spec.replicas = %d`, expectedReplica)),
				WithCondition(And(
					jq.Match(`.spec.replicas == %d`, expectedReplica),        // Spec updated
					jq.Match(`.status.readyReplicas == %d`, expectedReplica), // Pods ready
				)),
				WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),               // 7 minutes
				WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval), // 10s
			)
		})
	}
}

// ValidateCRDsReinstated ensures that the CRDs are properly removed and reinstated when a component is disabled and re-enabled.
func (tc *ComponentTestCtx) ValidateCRDsReinstated(t *testing.T, crds []CRD) {
	t.Helper()

	// Disable the component first and validate that all CRDs are removed
	tc.ValidateComponentDisabled(t)

	// Check that all CRDs are removed
	for _, crd := range crds {
		t.Run(crd.Name+"_removal", func(t *testing.T) {
			tc.ValidateCRDRemoval(crd.Name)
		})
	}

	// Enable the component now and validate that all CRDs are reinstated
	tc.ValidateComponentEnabled(t)

	// Check that all CRDs are reinstated
	for _, crd := range crds {
		t.Run(crd.Name+"_reinstatement", func(t *testing.T) {
			t.Parallel()
			tc.ValidateCRDReinstatement(crd.Name, crd.Version)
		})
	}
}

// ValidateComponentReleases ensures that the component releases exist and have valid fields.
func (tc *ComponentTestCtx) ValidateComponentReleases(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// Map DataSciencePipelines to aipipelines for v2 API
	componentFieldName := componentName
	if tc.GVK.Kind == dataSciencePipelinesKind {
		componentFieldName = aiPipelinesFieldName
	}

	// Ensure the DataScienceCluster exists and the component's conditions are met
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			And(
				// Ensure the component's management state is "Managed"
				jq.Match(`.spec.components.%s.managementState == "%s"`, componentFieldName, operatorv1.Managed),

				// Validate that the releases field contains at least one release for the component
				jq.Match(`.status.components.%s.releases | length > 0`, componentFieldName),

				// Validate the fields (name, version, repoUrl) for each release
				// No need to check for length here, the previous check validates if any release exists
				And(
					jq.Match(`.status.components.%s.releases[].name != ""`, componentFieldName),
					jq.Match(`.status.components.%s.releases[].version != ""`, componentFieldName),
					jq.Match(`.status.components.%s.releases[].repoUrl != ""`, componentFieldName),
				),
			),
		),
	)
}

// EnsureParentComponentEnabled ensures that the parent component is enabled and ready before enabling a subcomponent.
// getParentGVK maps a parent component kind string to its GVK.
func getParentGVK(parentKind string) (schema.GroupVersionKind, error) {
	switch parentKind {
	case "Kserve":
		return gvk.Kserve, nil
	case "Dashboard":
		return gvk.Dashboard, nil
	case "ModelRegistry":
		return gvk.ModelRegistry, nil
	default:
		return schema.GroupVersionKind{}, fmt.Errorf("unknown parent component kind: %s", parentKind)
	}
}

func (tc *ComponentTestCtx) EnsureParentComponentEnabled(t *testing.T) {
	t.Helper()

	if tc.ParentKind == "" {
		t.Fatal("EnsureParentComponentEnabled called on a component without parent information.")
	}

	// Enable the parent component in the DSC
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, tc.ParentKind)

	// Wait for the parent component CR to become ready
	// This is critical - without waiting, subcomponent tests may run while parent resources (ServiceAccounts, Secrets) are still being created
	parentComponentName, _ := getComponentNameFromKind(tc.ParentKind)

	// Map parent kind string to GVK
	parentGVK, err := getParentGVK(tc.ParentKind)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Waiting for parent component %s to become ready before proceeding with subcomponent tests", tc.ParentKind)
	tc.EnsureResourcesExist(
		WithMinimalObject(parentGVK, types.NamespacedName{Name: parentComponentName}),
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
	t.Logf("Parent component %s is ready - proceeding with subcomponent tests", tc.ParentKind)
}

// EnsureOperatorControllerRunning verifies that the operator controller is running before deletion recovery tests.
// Deletion recovery tests depend on the controller to recreate deleted resources. If the controller isn't running,
// tests will timeout waiting for recreation that will never happen (e.g., RHOAI e2e failure with 605s timeout).
// This check fails fast with a clear error instead of wasting 10+ minutes on timeouts.
func (tc *ComponentTestCtx) EnsureOperatorControllerRunning(t *testing.T) {
	t.Helper()

	deploymentName := tc.getControllerDeploymentName()

	// Check that the operator deployment exists and has ready replicas
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      deploymentName,
		}),
		WithCondition(And(
			jq.Match(`.status.readyReplicas != null`),
			jq.Match(`.status.readyReplicas > 0`),
		)),
		WithCustomErrorMsg("[INFRASTRUCTURE] Operator controller deployment %s has no ready replicas - deletion recovery tests will fail. Check operator deployment status.",
			deploymentName),
	)

	// Verify at least one operator pod is running
	pods := tc.FetchResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: tc.OperatorNamespace,
			LabelSelector: k8slabels.SelectorFromSet(map[string]string{
				"control-plane": "controller-manager",
			}),
		}),
	)

	if len(pods) == 0 {
		t.Fatalf("[INFRASTRUCTURE] No operator controller pods found in namespace %s - deletion recovery tests will fail. Controller must be running to recreate deleted resources.",
			tc.OperatorNamespace)
	}

	t.Logf("Operator controller verified: %d pod(s) running in namespace %s", len(pods), tc.OperatorNamespace)
}

// UpdateSubComponentStateInDataScienceCluster updates the management state of a subcomponent in the DataScienceCluster.
func (tc *ComponentTestCtx) UpdateSubComponentStateInDataScienceCluster(t *testing.T, state operatorv1.ManagementState) {
	t.Helper()

	if tc.ParentKind == "" || tc.SubComponentFieldName == "" {
		t.Fatal("UpdateSubComponentStateInDataScienceCluster called on a component without parent/subcomponent information.")
	}

	parentComponentName, parentConditionKind := getComponentNameFromKind(tc.ParentKind)
	subComponentName := tc.SubComponentFieldName

	readyCondition := metav1.ConditionFalse
	if state == operatorv1.Managed {
		readyCondition = metav1.ConditionTrue
	}

	// Define common conditions to match.
	conditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.%s.managementState == "%s"`, parentComponentName, subComponentName, state),
	}

	if readyCondition == metav1.ConditionTrue {
		// If the component is managed, the parent component should be ready
		conditions = append(conditions,
			// Validate the "Ready" condition for the parent component
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, parentConditionKind, metav1.ConditionTrue),
		)
	}

	conditions = append(conditions,
		// Validate the "Ready" condition for the subcomponent
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, readyCondition),
	)

	// Update the subcomponent's management state
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.%s.managementState = "%s"`, parentComponentName, subComponentName, state)),
		WithCondition(And(conditions...)),
	)
}

// ValidateSubComponentEnabled ensures that a subcomponent is enabled and its status is "Ready".
func (tc *ComponentTestCtx) ValidateSubComponentEnabled(t *testing.T) {
	t.Helper()

	if tc.ParentKind == "" || tc.SubComponentFieldName == "" {
		t.Fatal("ValidateSubComponentEnabled called on a component without parent/subcomponent information.")
	}

	// First, ensure the parent component is enabled and ready
	tc.EnsureParentComponentEnabled(t)

	// Enable the subcomponent
	tc.UpdateSubComponentStateInDataScienceCluster(t, operatorv1.Managed)

	// Ensure the subcomponent resource exists and is marked "Ready"
	tc.EnsureResourcesExist(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			And(
				HaveLen(1),
				HaveEach(And(
					jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
				)),
			),
		),
	)
}

// ValidateSubComponentReleases ensures that the subcomponent releases exist and have valid fields.
func (tc *ComponentTestCtx) ValidateSubComponentReleases(t *testing.T) {
	t.Helper()

	if tc.ParentKind == "" || tc.SubComponentFieldName == "" {
		t.Fatal("ValidateSubComponentReleases called on a component without parent/subcomponent information.")
	}

	parentComponentName, _ := getComponentNameFromKind(tc.ParentKind)
	subComponentName := tc.SubComponentFieldName

	// Ensure the DataScienceCluster exists and the parent component's conditions are met
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			And(
				// Ensure the parent component's management state is "Managed"
				jq.Match(`.spec.components.%s.managementState == "%s"`, parentComponentName, operatorv1.Managed),
				// Ensure the subcomponent's management state is "Managed"
				jq.Match(`.spec.components.%s.%s.managementState == "%s"`, parentComponentName, subComponentName, operatorv1.Managed),
				// Validate that the releases field contains at least one release for the parent component
				jq.Match(`.status.components.%s.releases | length > 0`, parentComponentName),
				// Validate the fields (name, version, repoUrl) for each release
				And(
					jq.Match(`.status.components.%s.releases[].name != ""`, parentComponentName),
					jq.Match(`.status.components.%s.releases[].version != ""`, parentComponentName),
					jq.Match(`.status.components.%s.releases[].repoUrl != ""`, parentComponentName),
				),
			),
		),
	)
}

// ValidateSubComponentDisabled ensures that a subcomponent is disabled and its resources are deleted.
func (tc *ComponentTestCtx) ValidateSubComponentDisabled(t *testing.T) {
	t.Helper()

	if tc.ParentKind == "" || tc.SubComponentFieldName == "" {
		t.Fatal("ValidateSubComponentDisabled called on a component without parent/subcomponent information.")
	}

	// Ensure that the resources associated with the subcomponent exist
	tc.EnsureResourcesExist(WithMinimalObject(tc.GVK, tc.NamespacedName))

	// Disable the subcomponent
	tc.UpdateSubComponentStateInDataScienceCluster(t, operatorv1.Removed)

	// Ensure that any Deployment resources for the subcomponent are not present
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	// Ensure that the resources associated with the subcomponent do not exist
	tc.EnsureResourcesGone(WithMinimalObject(tc.GVK, tc.NamespacedName))
}

// ValidateComponentCondition ensures that the specified component instance has the expected condition set to "True".
func (tc *ComponentTestCtx) ValidateComponentCondition(gvk schema.GroupVersionKind, componentName, statusType string) {
	tc.EnsureResourceExists(
		WithMinimalObject(gvk, types.NamespacedName{Name: componentName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, statusType, metav1.ConditionTrue)),
	)
}

// UpdateComponentStateInDataScienceCluster updates the management state of a specified component in the DataScienceCluster.
func (tc *ComponentTestCtx) UpdateComponentStateInDataScienceCluster(state operatorv1.ManagementState) {
	tc.UpdateComponentStateInDataScienceClusterWithKind(state, tc.GVK.Kind)
}

// UpdateComponentStateInDataScienceClusterWithKind updates the management state of a specified component kind in the DataScienceCluster.
func (tc *ComponentTestCtx) UpdateComponentStateInDataScienceClusterWithKind(state operatorv1.ManagementState, kind string) {
	// Delegate to the base TestContext method
	tc.TestContext.UpdateComponentStateInDataScienceClusterWithKind(state, kind)
}

// ValidateCRDRemoval ensures that the CRD is properly removed when the component is disabled.
func (tc *ComponentTestCtx) ValidateCRDRemoval(name string) {
	nn := types.NamespacedName{Name: name}

	// Ensure the CustomResourceDefinition (CRD) exists before deletion
	tc.EnsureResourceExists(WithMinimalObject(gvk.CustomResourceDefinition, nn))

	// Delete the CRD
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, nn),
		WithForegroundDeletion(),
		WithWaitForDeletion(true),
	)
}

// ValidateCRDReinstatement ensures that the CRD is properly reinstated when the component is enabled.
func (tc *ComponentTestCtx) ValidateCRDReinstatement(name string, version string) {
	nn := types.NamespacedName{Name: name}

	// Ensure the CRD is recreated
	tc.EnsureResourceExists(WithMinimalObject(gvk.CustomResourceDefinition, nn))

	// Ensure the CRD has the specified version
	if len(version) != 0 {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: name}),
			WithCondition(jq.Match(`.status.storedVersions[0] == "%s"`, version)),
		)
	}
}

// ValidateModelControllerInstance validates the existence and correct status of the ModelController and DataScienceCluster.
func (tc *ComponentTestCtx) ValidateModelControllerInstance(t *testing.T) {
	t.Helper()

	// Ensure ModelController resource exists with the expected owner references and status phase.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ModelController, types.NamespacedName{Name: componentApi.ModelControllerInstanceName}),
		WithCondition(
			And(
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
				jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady),
			),
		),
	)

	// Ensure ModelController condition matches the expected status in the DataScienceCluster.
	tc.ValidateComponentCondition(
		gvk.DataScienceCluster,
		tc.DataScienceClusterNamespacedName.Name,
		modelcontroller.ReadyConditionType,
	)
}

// ValidateAllDeletionRecovery runs the standard set of deletion recovery tests.
// The order of tests is carefully designed to handle dependencies and avoid timing issues.
func (tc *ComponentTestCtx) ValidateAllDeletionRecovery(t *testing.T) {
	t.Helper()

	// For subcomponents, ensure the parent component is enabled before deletion recovery tests
	// This prevents failures when the parent was disabled by earlier test groups (e.g., kserve disabled in group_1)
	if tc.ParentKind != "" {
		t.Logf("Subcomponent %s detected - ensuring parent component %s is enabled before deletion recovery tests", tc.GVK.Kind, tc.ParentKind)
		tc.EnsureParentComponentEnabled(t)
	}

	// Ensure operator controller is running before deletion recovery tests
	// Deletion recovery tests validate that the controller recreates resources after deletion
	// If the controller isn't running, tests will timeout waiting for recreation that will never happen
	// Fail fast with clear error instead of wasting 10+ minutes
	t.Logf("Verifying operator controller is running before deletion recovery tests")
	tc.EnsureOperatorControllerRunning(t)

	// Increase the global eventually timeout for deletion recovery tests
	// Use longEventuallyTimeout to handle controller performance under load and complex resource dependencies
	reset := tc.OverrideEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout, tc.TestTimeouts.defaultEventuallyPollInterval)
	defer reset() // Make sure it's reset after all tests run

	// Test order explanation:
	// 1. ConfigMaps/Services - these are stateless resources with no dependencies
	// 2. RBAC - establishes permissions that ServiceAccounts and Pods will need
	// 3. ServiceAccounts - creates identity tokens that Pods will use for API access
	// 4. Deployments - recreates Pods that depend on both RBAC permissions and SA tokens
	//
	// This ordering prevents the race condition where Pods restart before having proper
	// RBAC permissions, which causes "Unauthorized" errors and CrashLoopBackOff states.
	// The Deployment deletion/recreation at the end ensures all Pods get fresh SA tokens
	// without needing explicit Pod restarts in the ServiceAccount test.
	testCases := []TestCase{
		{"ConfigMap deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.ValidateResourceDeletionRecovery(t, gvk.ConfigMap, types.NamespacedName{Namespace: tc.AppsNamespace})
		}},
		{"Service deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.ValidateResourceDeletionRecovery(t, gvk.Service, types.NamespacedName{Namespace: tc.AppsNamespace})
		}},
		{"RBAC deletion recovery", tc.ValidateRBACDeletionRecovery},
		{"ServiceAccount deletion recovery", tc.ValidateServiceAccountDeletionRecovery},
		{"Deployment deletion recovery", tc.ValidateDeploymentDeletionRecovery},
	}

	RunTestCases(t, testCases)
}

// ValidateResourceDeletionRecovery validates that resources of a specific type are recreated upon deletion.
func (tc *ComponentTestCtx) ValidateResourceDeletionRecovery(t *testing.T, resourceGVK schema.GroupVersionKind, nn types.NamespacedName) {
	t.Helper()

	// Fetch existing resources of this type
	listOptions := &client.ListOptions{
		LabelSelector: k8slabels.Set{
			labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
		}.AsSelector(),
		Namespace: nn.Namespace,
	}

	existingResources := tc.FetchResources(
		WithMinimalObject(resourceGVK, nn),
		WithListOptions(listOptions),
	)

	if len(existingResources) == 0 {
		t.Logf("No %s resources found for component %s, skipping", resourceGVK.Kind, tc.GVK.Kind)
		return
	}

	// For each resource, test individual deletion-recreation
	for _, resource := range existingResources {
		resourceName := resource.GetName()
		resourceNamespace := resource.GetNamespace()

		t.Run(resourceGVK.Kind+"_"+resourceName, func(t *testing.T) {
			t.Helper()

			// Log start time for duration tracking
			startTime := time.Now()
			t.Logf("[DELETION-RECOVERY] Starting test for %s/%s", resourceGVK.Kind, resourceName)

			// Use OnFailure callback for diagnostic collection
			// This runs ONLY when Eventually() times out, avoiding the defer timing issue
			tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(resourceGVK, resources.NamespacedNameFromObject(&resource)),
				WithOnFailure(func() string {
					t.Logf("\n⚠️  Deletion recovery test FAILED - collecting diagnostics...")
					// Run diagnostics to understand why controller didn't recreate the resource
					// For subcomponents, use ParentKind instead of GVK.Kind to get the actual component
					componentKindForDiagnostic := tc.GVK.Kind
					if tc.ParentKind != "" {
						componentKindForDiagnostic = tc.ParentKind
						t.Logf("Using parent component %s for diagnostics (subcomponent: %s)", tc.ParentKind, tc.GVK.Kind)
					}
					diagnoseDeletionRecoveryFailure(
						tc.TestContext,
						resourceGVK,
						resourceName,
						resourceNamespace,
						componentKindForDiagnostic,
					)
					// Return failure message with controller tag
					return fmt.Sprintf("[CONTROLLER] %s %s was not recreated after deletion - controller may not be watching deletion events",
						resourceGVK.Kind, resourceName)
				}),
			)

			// Log success with duration
			duration := time.Since(startTime)
			t.Logf("[DELETION-RECOVERY] ✓ Success: %s/%s recreated in %v", resourceGVK.Kind, resourceName, duration)
		})
	}
}

// CaptureControllerDiagnostics performs comprehensive diagnostics on the controller state.
func (tc *ComponentTestCtx) CaptureControllerDiagnostics(ctx context.Context, t *testing.T) (*ControllerDiagnostics, error) {
	t.Helper()

	diag := &ControllerDiagnostics{
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}

	t.Log("================================================================================")
	t.Log("[COMPREHENSIVE-DIAGNOSTICS] Capturing complete controller state")
	t.Log("================================================================================")

	// SECTION 1: Component CR State
	t.Log("[1/6] Analyzing component CR state...")
	resource := tc.FetchResources(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
	)[0]

	diag.Generation, _, _ = unstructured.NestedInt64(resource.Object, "metadata", "generation")
	diag.ObservedGeneration, _, _ = unstructured.NestedInt64(resource.Object, "status", "observedGeneration")
	diag.ResourceVersion = resource.GetResourceVersion()
	diag.GenerationGap = diag.Generation - diag.ObservedGeneration
	diag.Labels = resource.GetLabels()
	diag.Annotations = resource.GetAnnotations()
	diag.Finalizers = resource.GetFinalizers()

	t.Logf("  Generation: %d", diag.Generation)
	t.Logf("  ObservedGeneration: %d", diag.ObservedGeneration)
	t.Logf("  Gap: %d %s", diag.GenerationGap, gapAssessment(diag.GenerationGap))
	t.Logf("  ResourceVersion: %s", diag.ResourceVersion)

	// SECTION 2: Conditions Analysis
	t.Log("[2/6] Analyzing status conditions...")
	diag.Conditions = extractConditions(&resource)
	if len(diag.Conditions) > 0 {
		now := time.Now()
		diag.MostRecentConditionAge = now.Sub(diag.Conditions[0].LastTransitionTime)
		diag.OldestConditionAge = now.Sub(diag.Conditions[len(diag.Conditions)-1].LastTransitionTime)

		t.Logf("  Total conditions: %d", len(diag.Conditions))
		t.Logf("  Most recent update: %v ago", diag.MostRecentConditionAge.Round(time.Second))
		t.Logf("  Oldest update: %v ago", diag.OldestConditionAge.Round(time.Second))

		for i, cond := range diag.Conditions {
			t.Logf("    [%d] %s=%s (reason: %s, age: %v, observedGen: %d)",
				i, cond.Type, cond.Status, cond.Reason, cond.Age.Round(time.Second), cond.ObservedGeneration)
			if cond.Message != "" {
				t.Logf("        Message: %s", cond.Message)
			}
		}
	} else {
		t.Log("  WARNING: No conditions found!")
		diag.Issues = append(diag.Issues, "No status conditions - controller may not have reconciled")
	}

	// SECTION 3: Owner References & Dependencies
	t.Log("[3/6] Analyzing owner references and dependencies...")
	diag.OwnerReferences = extractOwnerReferences(&resource)

	if len(diag.OwnerReferences) == 0 {
		t.Log("  WARNING: No owner references - controller may not manage this resource!")
		diag.Issues = append(diag.Issues, "No owner references - deletion may not be detected")
	} else {
		t.Logf("  Owner references: %d", len(diag.OwnerReferences))
		for i, owner := range diag.OwnerReferences {
			t.Logf("    [%d] %s/%s (controller=%v, blockDeletion=%v)",
				i, owner.Kind, owner.Name, owner.Controller, owner.BlockOwnerDeletion)
		}
	}

	if len(diag.Finalizers) > 0 {
		t.Logf("  Finalizers: %v", diag.Finalizers)
	}

	// SECTION 4: Parent Component Analysis (for subcomponents)
	if tc.ParentKind != "" {
		t.Logf("[4/6] Analyzing parent component (%s)...", tc.ParentKind)
		analyzeParentComponent(t, tc, diag)
	} else {
		t.Log("[4/6] No parent component (not a subcomponent)")
	}

	// SECTION 5: Operator Pod Health
	t.Log("[5/6] Analyzing operator pod health...")
	analyzeOperatorPods(t, tc, diag)

	// SECTION 6: Trigger Reconciliation Test
	t.Log("[6/6] Testing controller responsiveness (trigger reconciliation)...")
	testControllerResponsiveness(ctx, t, tc, &resource, diag)

	// Assessment
	t.Log("[ASSESSMENT] Generating health assessment...")
	assessControllerHealth(diag)

	t.Log("================================================================================")
	t.Logf("[DIAGNOSTICS-SUMMARY] Health: %v", diag.IsHealthy)
	if len(diag.Issues) > 0 {
		t.Logf("[DIAGNOSTICS-ISSUES] Found %d issues:", len(diag.Issues))
		for i, issue := range diag.Issues {
			t.Logf("  [%d] %s", i+1, issue)
		}
	}
	t.Logf("[DIAGNOSTICS-RECOMMENDATION] %s", diag.Recommendation)
	t.Log("================================================================================")

	return diag, nil
}

// analyzeParentComponent analyzes parent component state for subcomponents.
func analyzeParentComponent(t *testing.T, tc *ComponentTestCtx, diag *ControllerDiagnostics) {
	t.Helper()
	parentGVK, err := getParentGVK(tc.ParentKind)
	if err != nil {
		t.Logf("  ERROR: Unknown parent kind: %s", tc.ParentKind)
		diag.Issues = append(diag.Issues, fmt.Sprintf("Unknown parent kind: %s", tc.ParentKind))
		return
	}

	parentName, _ := getComponentNameFromKind(tc.ParentKind)
	parentResources := tc.FetchResources(
		WithMinimalObject(parentGVK, types.NamespacedName{Name: parentName}),
	)

	if len(parentResources) == 0 {
		t.Log("  ERROR: Parent component CR not found!")
		diag.ParentExists = false
		diag.Issues = append(diag.Issues, fmt.Sprintf("Parent component %s not found", tc.ParentKind))
		return
	}

	diag.ParentExists = true
	parent := parentResources[0]

	diag.ParentGeneration, _, _ = unstructured.NestedInt64(parent.Object, "metadata", "generation")
	diag.ParentObservedGen, _, _ = unstructured.NestedInt64(parent.Object, "status", "observedGeneration")
	diag.ParentManagementState, _, _ = unstructured.NestedString(parent.Object, "spec", "managementState")
	diag.ParentConditions = extractConditions(&parent)

	t.Logf("  Parent exists: %s", tc.ParentKind)
	t.Logf("  Parent managementState: %s", diag.ParentManagementState)
	t.Logf("  Parent generation: %d (observed: %d, gap: %d)",
		diag.ParentGeneration, diag.ParentObservedGen, diag.ParentGeneration-diag.ParentObservedGen)

	if diag.ParentManagementState != "Managed" {
		t.Logf("  WARNING: Parent not in Managed state - may not manage subcomponents!")
		diag.Issues = append(diag.Issues, fmt.Sprintf("Parent %s not in Managed state: %s", tc.ParentKind, diag.ParentManagementState))
	}

	if len(diag.ParentConditions) > 0 {
		t.Logf("  Parent conditions: %d", len(diag.ParentConditions))
		for i, cond := range diag.ParentConditions {
			t.Logf("    [%d] %s=%s (age: %v)", i, cond.Type, cond.Status, cond.Age.Round(time.Second))
		}
	}
}

// analyzeOperatorPods analyzes operator pod health.
func analyzeOperatorPods(t *testing.T, tc *ComponentTestCtx, diag *ControllerDiagnostics) {
	t.Helper()
	pods := tc.FetchResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: tc.OperatorNamespace,
			LabelSelector: k8slabels.SelectorFromSet(k8slabels.Set{
				"control-plane": "controller-manager",
			}),
		}),
	)

	diag.OperatorPodCount = len(pods)
	diag.OperatorPodsRunning = 0

	t.Logf("  Operator pods found: %d", len(pods))

	for _, pod := range pods {
		podDiag := PodStatusDiagnostic{
			Name: pod.GetName(),
		}

		phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
		podDiag.Phase = phase

		if phase == "Running" {
			diag.OperatorPodsRunning++
			podDiag.Ready = true
		}

		// Get creation time for age
		creationTime := pod.GetCreationTimestamp()
		podDiag.Age = time.Since(creationTime.Time)

		t.Logf("    [%s] Phase=%s, Age=%v",
			podDiag.Name, podDiag.Phase, podDiag.Age.Round(time.Second))

		diag.OperatorPodStatuses = append(diag.OperatorPodStatuses, podDiag)
	}

	t.Logf("  Operator pods running: %d/%d", diag.OperatorPodsRunning, diag.OperatorPodCount)

	if diag.OperatorPodCount == 0 {
		diag.Issues = append(diag.Issues, "No operator pods found!")
	} else if diag.OperatorPodsRunning == 0 {
		diag.Issues = append(diag.Issues, "Operator pods exist but none are Running")
	}
}

// testControllerResponsiveness tests if controller responds to triggered reconciliation.
func testControllerResponsiveness(ctx context.Context, t *testing.T, tc *ComponentTestCtx, resource *unstructured.Unstructured, diag *ControllerDiagnostics) {
	t.Helper()
	currentObservedGen := diag.ObservedGeneration

	// Add test annotation to trigger reconciliation
	annotations := resource.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	testAnnotationKey := "test.opendatahub.io/reconciliation-check"
	testAnnotationValue := time.Now().Format(time.RFC3339)
	annotations[testAnnotationKey] = testAnnotationValue
	resource.SetAnnotations(annotations)

	t.Logf("  Triggering reconciliation with annotation: %s=%s", testAnnotationKey, testAnnotationValue)

	startTime := time.Now()
	err := tc.TestContext.Client().Update(ctx, resource)
	if err != nil {
		t.Logf("  ERROR: Failed to update resource: %v", err)
		diag.TriggeredReconciliation = false
		diag.Issues = append(diag.Issues, fmt.Sprintf("Failed to trigger reconciliation: %v", err))
		return
	}

	diag.TriggeredReconciliation = true

	// Wait for controller to respond (max 90 seconds)
	timeout := 90 * time.Second
	checkInterval := 2 * time.Second

	for elapsed := time.Duration(0); elapsed < timeout; elapsed += checkInterval {
		time.Sleep(checkInterval)

		// Fetch updated resource
		updated := tc.FetchResources(
			WithMinimalObject(tc.GVK, tc.NamespacedName),
		)[0]

		newObservedGen, _, _ := unstructured.NestedInt64(updated.Object, "status", "observedGeneration")

		if newObservedGen > currentObservedGen {
			diag.ReconciliationLatency = time.Since(startTime)
			diag.ReconciliationSuccess = true

			t.Logf("  SUCCESS: Controller responded in %v (observedGeneration: %d -> %d)",
				diag.ReconciliationLatency.Round(100*time.Millisecond), currentObservedGen, newObservedGen)

			// Clean up test annotation
			annotations = updated.GetAnnotations()
			delete(annotations, testAnnotationKey)
			updated.SetAnnotations(annotations)
			_ = tc.TestContext.Client().Update(ctx, &updated)

			return
		}
	}

	// Timeout - controller didn't respond
	diag.ReconciliationSuccess = false
	diag.ReconciliationLatency = time.Since(startTime)

	t.Logf("  FAILURE: Controller did not respond within %v", timeout)
	diag.Issues = append(diag.Issues, fmt.Sprintf("Controller did not respond to triggered reconciliation within %v", timeout))
}

// assessControllerHealth generates health assessment and recommendation.
func assessControllerHealth(diag *ControllerDiagnostics) {
	// Start optimistic
	diag.IsHealthy = true

	// Check for critical issues
	if diag.OperatorPodsRunning == 0 {
		diag.IsHealthy = false
		diag.Recommendation = "FAIL-FAST: No operator pods running - infrastructure failure"
		return
	}

	if !diag.TriggeredReconciliation || !diag.ReconciliationSuccess {
		diag.IsHealthy = false
		if diag.ReconciliationLatency > 60*time.Second {
			diag.Recommendation = "FAIL-FAST: Controller not responding - may be crashed/stalled/in-backoff"
		} else {
			diag.Recommendation = "WARNING: Controller slow to respond - may be under load"
		}
		return
	}

	if diag.GenerationGap > 2 {
		diag.Recommendation = "WARNING: Controller significantly behind (gap > 2) - may be catching up"
		return
	}

	if len(diag.OwnerReferences) == 0 {
		diag.Recommendation = "WARNING: No owner references - deletion may not trigger recreation"
		return
	}

	// All checks passed
	diag.Recommendation = "PROCEED: Controller is healthy and responsive"
}

// gapAssessment provides human-readable assessment of generation gap.
func gapAssessment(gap int64) string {
	if gap == 0 {
		return "✓ (in sync)"
	} else if gap == 1 {
		return "(reconciliation in progress)"
	} else if gap <= 3 {
		return "⚠ (controller catching up)"
	}
	return "✗ (controller significantly behind)"
}

// extractConditions extracts and sorts conditions from a resource.
func extractConditions(resource *unstructured.Unstructured) []ConditionDiagnostic {
	var result []ConditionDiagnostic

	conditions, found, _ := unstructured.NestedSlice(resource.Object, "status", "conditions")
	if !found {
		return result
	}

	now := time.Now()
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}

		condDiag := ConditionDiagnostic{}
		condDiag.Type, _, _ = unstructured.NestedString(condMap, "type")
		condDiag.Status, _, _ = unstructured.NestedString(condMap, "status")
		condDiag.Reason, _, _ = unstructured.NestedString(condMap, "reason")
		condDiag.Message, _, _ = unstructured.NestedString(condMap, "message")
		condDiag.ObservedGeneration, _, _ = unstructured.NestedInt64(condMap, "observedGeneration")

		lastTransition, _, _ := unstructured.NestedString(condMap, "lastTransitionTime")
		if lastTransition != "" {
			if t, err := time.Parse(time.RFC3339, lastTransition); err == nil {
				condDiag.LastTransitionTime = t
				condDiag.Age = now.Sub(t)
			}
		}

		result = append(result, condDiag)
	}

	// Sort by most recent first
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastTransitionTime.After(result[j].LastTransitionTime)
	})

	return result
}

// extractOwnerReferences extracts owner references from a resource.
func extractOwnerReferences(resource *unstructured.Unstructured) []OwnerRefDiagnostic {
	var result []OwnerRefDiagnostic

	ownerRefs := resource.GetOwnerReferences()
	for _, ref := range ownerRefs {
		ownerDiag := OwnerRefDiagnostic{
			APIVersion: ref.APIVersion,
			Kind:       ref.Kind,
			Name:       ref.Name,
			UID:        string(ref.UID),
		}

		if ref.Controller != nil {
			ownerDiag.Controller = *ref.Controller
		}
		if ref.BlockOwnerDeletion != nil {
			ownerDiag.BlockOwnerDeletion = *ref.BlockOwnerDeletion
		}

		result = append(result, ownerDiag)
	}

	return result
}

// formatDiagnosticIssues formats a list of diagnostic issues for error messages.
func formatDiagnosticIssues(issues []string) string {
	if len(issues) == 0 {
		return "  (no specific issues detected)"
	}

	var formatted strings.Builder
	for i, issue := range issues {
		formatted.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, issue))
	}
	return formatted.String()
}

// ValidateDeploymentDeletionRecovery validates Deployment resources are recreated upon deletion.
func (tc *ComponentTestCtx) ValidateDeploymentDeletionRecovery(t *testing.T) {
	t.Helper()

	// Fetch Deployments
	deployments := tc.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
	)

	if len(deployments) == 0 {
		t.Logf("No Deployment resources found for component %s, skipping deletion recovery test", tc.GVK.Kind)
		return
	}

	// For each Deployment, delete it and verify it gets recreated with robust deletion-recreation testing
	for _, deployment := range deployments {
		t.Run("deployment_"+deployment.GetName(), func(t *testing.T) {
			t.Helper()

			// Ensure the deployment's ServiceAccount exists before running deletion recovery test
			// This prevents race conditions where parent component is "Ready" but hasn't created
			// dependent resources yet (especially in Hypershift clusters)
			saName, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "serviceAccountName")
			if err == nil && found && saName != "" {
				t.Logf("Ensuring ServiceAccount %s exists before deployment deletion recovery test", saName)
				tc.EnsureResourceExists(
					WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{
						Name:      saName,
						Namespace: deployment.GetNamespace(),
					}),
					WithCustomErrorMsg("ServiceAccount %s must exist before Deployment %s deletion recovery test - parent component may not be fully provisioned",
						saName, deployment.GetName()),
				)
				t.Logf("ServiceAccount %s confirmed to exist", saName)
			}

			// COMPREHENSIVE CONTROLLER DIAGNOSTICS
			// Captures complete controller state including generation tracking, condition analysis,
			// owner references, parent component state, operator pod health, and actively tests
			// controller responsiveness by triggering reconciliation
			ctx := context.Background()
			diag, err := tc.CaptureControllerDiagnostics(ctx, t)
			if err != nil {
				t.Fatalf("Failed to capture controller diagnostics: %v", err)
			}

			// Fail fast if controller is not healthy
			if !diag.IsHealthy {
				if strings.Contains(diag.Recommendation, "FAIL-FAST") {
					t.Fatalf("[INFRASTRUCTURE] Controller diagnostics failed: %s\n\nIssues detected:\n%s",
						diag.Recommendation, formatDiagnosticIssues(diag.Issues))
				}
				// Just a warning - log but proceed
				t.Logf("[WARNING] Controller diagnostics show potential issues: %s", diag.Recommendation)
			}

			// Use robust deletion-recreation pattern that handles race conditions and verifies actual recreation
			// Deployments may have complex dependencies (CRDs, namespaces, DSCI) so use longer timeout
			recreatedDeployment := tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(&deployment)),
			)

			// Verify the recreated Deployment has proper conditions
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(recreatedDeployment)),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
					status.ConditionTypeAvailable, metav1.ConditionTrue)),
				WithCustomErrorMsg("Recreated deployment should have Available condition"),
			)
		})
	}
}

// ValidateServiceAccountDeletionRecovery validates ServiceAccount resources are recreated upon deletion.
func (tc *ComponentTestCtx) ValidateServiceAccountDeletionRecovery(t *testing.T) {
	t.Helper()
	// TODO: Re-enable after investigating token refresh timing issues
	t.Skip("Skipped due to token refresh timing issues")

	// Fetch ServiceAccounts
	serviceAccounts := tc.FetchResources(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
	)

	if len(serviceAccounts) == 0 {
		t.Logf("No ServiceAccount resources found for component %s, skipping deletion recovery test", tc.GVK.Kind)
		return
	}

	// For each ServiceAccount, delete it and verify it gets recreated with robust deletion-recreation testing
	for _, serviceAccount := range serviceAccounts {
		t.Run("serviceAccount_"+serviceAccount.GetName(), func(t *testing.T) {
			t.Helper()

			// Use robust deletion-recreation pattern that handles race conditions and verifies actual recreation
			tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(gvk.ServiceAccount, resources.NamespacedNameFromObject(&serviceAccount)),
			)
		})
	}
}

// ValidateRBACDeletionRecovery validates all RBAC resources (ClusterRole, ClusterRoleBinding, Role, RoleBinding)
// are recreated upon deletion. Tests RBAC resources sequentially to avoid dependency conflicts.
func (tc *ComponentTestCtx) ValidateRBACDeletionRecovery(t *testing.T) {
	t.Helper()
	// TODO: Re-enable after investigating external dependency timing issues
	t.Skip("Skipped due to external dependency timing issues")

	// RBAC resource types in dependency order (referenced resources first)
	rbacResourceTypes := []struct {
		gvk schema.GroupVersionKind
		nn  types.NamespacedName
	}{
		{gvk.ClusterRole, types.NamespacedName{}},
		{gvk.Role, types.NamespacedName{Namespace: tc.AppsNamespace}},
		{gvk.ClusterRoleBinding, types.NamespacedName{}},
		{gvk.RoleBinding, types.NamespacedName{Namespace: tc.AppsNamespace}},
	}

	// Test each RBAC resource type sequentially to avoid dependency conflicts
	for _, rbacType := range rbacResourceTypes {
		t.Run(rbacType.gvk.Kind+" deletion recovery", func(t *testing.T) {
			t.Helper()
			// Don't run in parallel due to RBAC interdependencies
			tc.ValidateResourceDeletionRecovery(t, rbacType.gvk, rbacType.nn)
		})
	}
}
