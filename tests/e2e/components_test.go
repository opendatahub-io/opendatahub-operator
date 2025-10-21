package e2e_test

import (
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
}

// CRD represents a custom resource definition with a name and version.
type CRD struct {
	Name    string
	Version string
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

	return &componentCtx, nil
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

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ValidatingAdmissionPolicy, types.NamespacedName{Name: "s3-secret-check-bucket-exist"}),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ValidatingAdmissionPolicyBinding, types.NamespacedName{Name: "s3-secret-check-bucket-exist-binding"}),
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

			// Update the deployment's replica count
			tc.ConsistentlyResourceCreatedOrUpdated(
				WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(&d)),
				WithMutateFunc(testf.Transform(`.spec.replicas = %d`, expectedReplica)),
				WithCondition(jq.Match(`.spec.replicas == %d`, expectedReplica)),
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
	componentName := strings.ToLower(kind)

	// Map DataSciencePipelines to aipipelines for v2 API
	componentFieldName := componentName
	conditionKind := kind
	if kind == dataSciencePipelinesKind {
		componentFieldName = aiPipelinesFieldName
		conditionKind = "AIPipelines"
	}

	readyCondition := metav1.ConditionFalse
	if state == operatorv1.Managed {
		readyCondition = metav1.ConditionTrue
	}

	// Define common conditions to match.
	conditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentFieldName, state),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, conditionKind, readyCondition),
	}

	// TODO: Commented out because this check does not work with parallel component tests.
	// Verify it is still needed, otherwise remove it. A new test only for those conditions is added in resilience tests.
	//
	// If the state is "Managed", add additional checks for provisioning and components readiness.
	// if state == operatorv1.Managed {
	// 	conditions = append(conditions,
	// 		// Validate the "ProvisioningSucceeded" condition
	// 		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, readyCondition),

	// 		// Validate the "ComponentsReady" condition
	// 		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeComponentsReady, readyCondition),
	// 	)
	// }

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentFieldName, state)),
		WithCondition(And(conditions...)),
	)
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
		t.Run(resourceGVK.Kind+"_"+resource.GetName(), func(t *testing.T) {
			t.Helper()

			tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(resourceGVK, resources.NamespacedNameFromObject(&resource)),
			)
		})
	}
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
