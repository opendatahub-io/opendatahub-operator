package e2e_test

import (
	"strings"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type ModelRegistryTestCtx struct {
	*ComponentTestCtx
}

func modelRegistryTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.ModelRegistry{})
	require.NoError(t, err)

	componentCtx := ModelRegistryTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate component spec", componentCtx.validateSpec)
	t.Run("Validate component conditions", componentCtx.validateConditions)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)

	t.Run("Validate watched resources", componentCtx.validateOperandsWatchedResources)
	t.Run("Validate dynamically watches operands", componentCtx.validateOperandsDynamicallyWatchedResources)
	t.Run("Validate CRDs reinstated", componentCtx.validateCRDReinstated)
	t.Run("Validate cert", componentCtx.validateModelRegistryCert)
	t.Run("Validate ServiceMeshMember", componentCtx.validateModelRegistryServiceMeshMember)

	t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

func (c *ModelRegistryTestCtx) validateSpec(t *testing.T) {
	g := c.NewWithT(t)

	dsc, err := c.GetDSC()
	g.Expect(err).NotTo(HaveOccurred())

	g.List(gvk.ModelRegistry).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.spec.registriesNamespace == "%s"`, dsc.Spec.Components.ModelRegistry.RegistriesNamespace),
		)),
	))
}

func (c *ModelRegistryTestCtx) validateConditions(t *testing.T) {
	g := c.NewWithT(t)

	g.List(gvk.ModelRegistry).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServiceMeshAvailable, metav1.ConditionTrue),
		)),
	))
}

func (c *ModelRegistryTestCtx) validateOperandsWatchedResources(t *testing.T) {
	g := c.NewWithT(t)

	g.List(
		gvk.ServiceMeshMember,
		client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
	).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata | has("ownerReferences") | not`),
		)),
	))
}

func (c *ModelRegistryTestCtx) validateOperandsDynamicallyWatchedResources(t *testing.T) {
	g := c.NewWithT(t)

	mri, err := g.Get(gvk.ModelRegistry, types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}).Get()
	g.Expect(err).ShouldNot(HaveOccurred())

	rn, err := jq.ExtractValue[string](mri, ".spec.registriesNamespace")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rn).ShouldNot(BeEmpty())

	newPt := xid.New().String()
	oldPt := ""

	g.Update(gvk.ServiceMeshMember, types.NamespacedName{Name: "default", Namespace: rn}, func(obj *unstructured.Unstructured) error {
		oldPt = resources.SetAnnotation(obj, annotations.PlatformType, newPt)
		return nil
	}).Eventually().Should(
		Succeed(),
	)

	g.List(
		gvk.ServiceMeshMember,
		client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
	).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, oldPt),
		)),
	))
}

func (c *ModelRegistryTestCtx) validateModelRegistryCert(t *testing.T) {
	g := c.NewWithT(t)

	dsci, err := g.Get(gvk.DSCInitialization, c.DSCIName).Get()
	g.Expect(err).ShouldNot(HaveOccurred())

	smns, err := jq.ExtractValue[string](dsci, ".spec.serviceMesh.controlPlane.namespace")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(smns).ShouldNot(BeEmpty())

	is, err := cluster.FindDefaultIngressSecret(g.Context(), g.Client())
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Get(gvk.Secret, types.NamespacedName{Namespace: smns, Name: modelregistryctrl.DefaultModelRegistryCert}).Eventually().Should(And(
		jq.Match(`.type == "%s"`, is.Type),
		jq.Match(`(.data."tls.crt" | @base64d) == "%s"`, is.Data["tls.crt"]),
		jq.Match(`(.data."tls.key" | @base64d) == "%s"`, is.Data["tls.key"]),
	))
}

func (c *ModelRegistryTestCtx) validateModelRegistryServiceMeshMember(t *testing.T) {
	g := c.NewWithT(t)

	mri, err := g.Get(gvk.ModelRegistry, types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}).Get()
	g.Expect(err).ShouldNot(HaveOccurred())

	rn, err := jq.ExtractValue[string](mri, ".spec.registriesNamespace")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rn).ShouldNot(BeEmpty())

	g.Get(gvk.ServiceMeshMember, types.NamespacedName{Namespace: rn, Name: "default"}).Eventually().Should(
		jq.Match(`.spec | has("controlPlaneRef")`),
	)
}

func (c *ModelRegistryTestCtx) validateCRDReinstated(t *testing.T) {
	crds := []string{"modelregistries.modelregistry.opendatahub.io"}

	for _, crd := range crds {
		t.Run(crd, func(t *testing.T) {
			c.ValidateCRDReinstated(t, crd)
		})
	}
}
