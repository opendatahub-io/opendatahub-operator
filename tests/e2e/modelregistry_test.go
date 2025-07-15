package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type ModelRegistryTestCtx struct {
	*ComponentTestCtx
}

func modelRegistryTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.ModelRegistry{})
	require.NoError(t, err)

	componentCtx := ModelRegistryTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component spec", componentCtx.ValidateSpec},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate cert should be created from default DSCI when servicmesh is Managed", componentCtx.ValidateModelRegistryCert},
		{"Validate SMM only created if servicemesh is Managed", componentCtx.ValidateSMM},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
		{"Validate namespace error reporting", componentCtx.ValidateNamespaceErrorReporting},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateSpec checks the ModelRegistry spec against the DataScienceCluster instance.
func (tc *ModelRegistryTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

	// Retrieve the DataScienceCluster instance.
	dsc := tc.FetchDataScienceCluster()

	// Validate that the registriesNamespace in ModelRegistry matches the corresponding value in DataScienceCluster spec.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ModelRegistry, types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}),
		WithCondition(jq.Match(`.spec.registriesNamespace == "%s"`, dsc.Spec.Components.ModelRegistry.RegistriesNamespace)),
	)
}

// ValidateModelRegistryCert validates the ModelRegistry certificate for the associated ServiceMesh.
func (tc *ModelRegistryTestCtx) ValidateModelRegistryCert(t *testing.T) {
	t.Helper()

	// Retrieve DSCInitialization resource
	dsci := tc.FetchDSCInitialization()

	if dsci.Spec.ServiceMesh != nil && dsci.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		// Ensure that the Service Mesh control plane namespace is not empty.
		tc.g.Expect(dsci.Spec.ServiceMesh.ControlPlane.Namespace).NotTo(BeEmpty())

		is, err := cluster.FindDefaultIngressSecret(tc.g.Context(), tc.g.Client())
		tc.g.Expect(err).NotTo(HaveOccurred())

		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Secret, types.NamespacedName{Namespace: dsci.Spec.ServiceMesh.ControlPlane.Namespace, Name: modelregistryctrl.DefaultModelRegistryCert}),
			WithCondition(And(
				jq.Match(`.type == "%s"`, is.Type),
				jq.Match(`(.data."tls.crt" | @base64d) == "%s"`, is.Data["tls.crt"]),
				jq.Match(`(.data."tls.key" | @base64d) == "%s"`, is.Data["tls.key"]),
			)),
		)
	}
}

func (tc *ModelRegistryTestCtx) ValidateSMM(t *testing.T) {
	t.Helper()

	// Retrieve the ModelRegistry instance.
	mri := tc.retrieveModelRegistry()

	// Ensure that the registries namespace is not empty.
	tc.g.Expect(mri.Spec.RegistriesNamespace).NotTo(BeEmpty())

	dsci := tc.FetchDSCInitialization()

	// Ensure that the ServiceMeshMember exists and matches the expected condition if ServiceMesh is enabled.
	if dsci.Spec.ServiceMesh != nil && dsci.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.ServiceMeshMember, types.NamespacedName{Namespace: mri.Spec.RegistriesNamespace, Name: serviceMeshMemberName}),
			WithCondition(jq.Match(`.spec | has("controlPlaneRef")`)),
		)
	} else {
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(gvk.ServiceMeshMember, types.NamespacedName{Name: serviceMeshMemberName, Namespace: mri.Spec.RegistriesNamespace}),
			WithCustomErrorMsg(`Ensuring there is no SMM created`),
		)
	}
}

// ValidateCRDReinstated ensures that required CRDs are reinstated if deleted.
func (tc *ModelRegistryTestCtx) ValidateCRDReinstated(t *testing.T) {
	t.Helper()

	crds := []CRD{
		{Name: "modelregistries.modelregistry.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

func (tc *ModelRegistryTestCtx) retrieveModelRegistry() *componentApi.ModelRegistry {
	mri := &componentApi.ModelRegistry{}
	tc.FetchTypedResource(
		mri,
		WithMinimalObject(gvk.ModelRegistry, types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}),
	)

	// Ensure that the registries namespace is not empty.
	tc.g.Expect(mri.Spec.RegistriesNamespace).NotTo(BeEmpty())

	return mri
}

// ValidateNamespaceErrorReporting tests how operator reports validation errors in status conditions.
func (tc *ModelRegistryTestCtx) ValidateNamespaceErrorReporting(t *testing.T) {
	t.Helper()

	t.Log("Testing operator validation error reporting")

	originalDSC := tc.FetchDataScienceCluster()

	// Restore original DSC state after the test
	defer func() {
		t.Log("Restoring original DSC state")
		tc.EnsureResourceCreatedOrUpdated(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.TransformPipeline(
				testf.Transform(`.spec.components.modelregistry.managementState = "%s"`, originalDSC.Spec.Components.ModelRegistry.ManagementState),
				testf.Transform(`.spec.components.modelregistry.registriesNamespace = "%s"`, originalDSC.Spec.Components.ModelRegistry.RegistriesNamespace),
			)),
		)
	}()

	t.Log("Creating DSC with ModelRegistry using an invalid namespace that operator will reject during reconciliation")
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.modelregistry.managementState = "Managed"`),
			testf.Transform(`.spec.components.modelregistry.registriesNamespace = "invalid--namespace"`),
		)),
	)

	t.Log("Checking that DSC reports ModelRegistryReady as False when ModelRegistry has validation errors")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(
			// Verify ModelRegistry is reported as not ready
			jq.Match(`.status.conditions[] | select(.type == "ModelRegistryReady") | .status == "False"`),
			// Verify error message is descriptive
			jq.Match(`.status.conditions[] | select(.type == "ModelRegistryReady") | .message != null`),
		)),
		WithCustomErrorMsg("Operator should report descriptive error for ModelRegistry validation failure"),
	)

	t.Log("Checking that DSC reports ComponentsReady as False when components have validation errors")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "ComponentsReady") | .status == "False"`)),
		WithCustomErrorMsg("DSC should report ComponentsReady as False when components have validation errors"),
	)

	t.Log("Operator properly reported validation errors in status conditions")
}
