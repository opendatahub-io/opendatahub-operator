package e2e_test

import (
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

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
		{"Validate watched resources", componentCtx.ValidateOperandsWatchedResources},
		{"Validate dynamically watches operands", componentCtx.ValidateOperandsDynamicallyWatchedResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate cert should be created from default DSCI", componentCtx.ValidateModelRegistryCert},
		{"Validate no SMM should be created", componentCtx.ValidateNoSMM},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
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

// ValidateOperandsWatchedResources validates the resources being watched by the operands.
func (tc *ModelRegistryTestCtx) ValidateOperandsWatchedResources(t *testing.T) {
	t.Helper()

	// Retrieve the ModelRegistry instance.
	mri := tc.retrieveModelRegistry()

	// Ensure the correct labels are set on the ServiceMeshMember and that ownerReferences are not present.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMeshMember, types.NamespacedName{Namespace: mri.Spec.RegistriesNamespace, Name: serviceMeshMemberName}),
		WithCondition(jq.Match(`.metadata | has("ownerReferences") | not`)),
	)
}

// ValidateOperandsDynamicallyWatchedResources validates the dynamic watching of operands.
func (tc *ModelRegistryTestCtx) ValidateOperandsDynamicallyWatchedResources(t *testing.T) {
	t.Helper()

	// Retrieve the ModelRegistry instance.
	mri := tc.retrieveModelRegistry()

	// Generate unique platform type values
	newPt := xid.New().String()
	oldPt := ""

	// Apply new platform type annotation and verify
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.ServiceMeshMember,
			types.NamespacedName{Namespace: mri.Spec.RegistriesNamespace, Name: serviceMeshMemberName},
		),
		WithMutateFunc(
			func(obj *unstructured.Unstructured) error {
				oldPt = resources.SetAnnotation(obj, annotations.PlatformType, newPt)
				return nil
			},
		),
	)

	// Ensure previously created resource retains their old platform type annotation
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMeshMember, types.NamespacedName{Namespace: mri.Spec.RegistriesNamespace, Name: serviceMeshMemberName}),
		WithCondition(jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, oldPt)),
	)
}

// ValidateModelRegistryCert validates the ModelRegistry certificate for the associated ServiceMesh.
func (tc *ModelRegistryTestCtx) ValidateModelRegistryCert(t *testing.T) {
	t.Helper()

	// Retrieve DSCInitialization resource
	dsci := tc.FetchDSCInitialization()

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

// ValidateNoSMM ensures there are no ServiceMeshMember.
func (tc *ModelRegistryTestCtx) ValidateNoSMM(t *testing.T) {
	t.Helper()
	// Retrieve the DataScienceCluster instance.
	dsc := tc.FetchDataScienceCluster()

	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.ServiceMeshMember, types.NamespacedName{Name: "default", Namespace: dsc.Spec.Components.ModelRegistry.RegistriesNamespace}),
		WithCustomErrorMsg(`Ensuring there is no SMM created`),
	)
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
