package e2e_test

import (
	"strings"
	"testing"
	"time"

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

	// Ensure that any Deployment resources for the component are present
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
	)

	// Ensure the component resource exists and is marked "Ready".
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
			tc.EventuallyResourceCreatedOrUpdated(
				WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(&d)),
				WithMutateFunc(testf.Transform(`.spec.replicas = %d`, expectedReplica)),
				WithCondition(jq.Match(`.spec.replicas == %d`, expectedReplica)),
			)

			tc.EnsureResourceExistsConsistently(
				WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(&d)),
				WithCondition(jq.Match(`.spec.replicas == %d`, expectedReplica)),
				WithConsistentlyDuration(30*time.Second),
				WithConsistentlyPollingInterval(1*time.Second),
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
		t.Run(crd.Name, func(t *testing.T) {
			tc.ValidateCRDRemoval(crd.Name)
		})
	}

	// Enable the component now and validate that all CRDs are reinstated
	tc.ValidateComponentEnabled(t)

	// Check that all CRDs are reinstated
	for _, crd := range crds {
		t.Run(crd.Name, func(t *testing.T) {
			tc.ValidateCRDReinstatement(crd.Name, crd.Version)
		})
	}
}

// ValidateComponentReleases ensures that the component releases exist and have valid fields.
func (tc *ComponentTestCtx) ValidateComponentReleases(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// Ensure the DataScienceCluster exists and the component's conditions are met
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			And(
				// Ensure the component's management state is "Managed"
				jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, operatorv1.Managed),

				// Validate that the releases field contains at least one release for the component
				jq.Match(`.status.components.%s.releases | length > 0`, componentName),

				// Validate the fields (name, version, repoUrl) for each release
				// No need to check for length here, the previous check validates if any release exists
				And(
					jq.Match(`.status.components.%s.releases[].name != ""`, componentName),
					jq.Match(`.status.components.%s.releases[].version != ""`, componentName),
					jq.Match(`.status.components.%s.releases[].repoUrl != ""`, componentName),
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
	tc.UpdateComponentStateInDataScienceClusterWhitKind(state, tc.GVK.Kind)
}

// UpdateComponentStateInDataScienceClusterWhitKind updates the management state of a specified component kind in the DataScienceCluster.
func (tc *ComponentTestCtx) UpdateComponentStateInDataScienceClusterWhitKind(state operatorv1.ManagementState, kind string) {
	componentName := strings.ToLower(kind)
	readyCondition := metav1.ConditionFalse
	if state == operatorv1.Managed {
		readyCondition = metav1.ConditionTrue
	}

	// Define common conditions to match.
	conditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, state),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, kind, readyCondition),
	}

	// If the state is "Managed", add additional checks for provisioning and components readiness.
	if state == operatorv1.Managed {
		conditions = append(conditions,
			// Validate the "ProvisioningSucceeded" condition
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, readyCondition),

			// Validate the "ComponentsReady" condition
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeComponentsReady, readyCondition),
		)
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, state)),
		WithCondition(And(conditions...)),
	)
}

// ValidateCRDRemoval ensures that the CRD is properly removed when the component is disabled.
func (tc *ComponentTestCtx) ValidateCRDRemoval(name string) {
	nn := types.NamespacedName{Name: name}

	// Ensure the CustomResourceDefinition (CRD) exists before deletion
	tc.EnsureResourceExists(WithMinimalObject(gvk.CustomResourceDefinition, nn))

	// Delete the CRD
	propagationPolicy := metav1.DeletePropagationForeground
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, nn),
		WithClientDeleteOptions(
			&client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			}),
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
