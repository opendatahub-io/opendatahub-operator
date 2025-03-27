package e2e_test

import (
	"strings"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func dashboardTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.Dashboard{})
	require.NoError(t, err)

	componentCtx := DashboardTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate dynamically watches operands", componentCtx.validateOperandsDynamicallyWatchedResources)
	t.Run("Validate CRDs reinstated", componentCtx.validateCRDReinstated)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

type DashboardTestCtx struct {
	*ComponentTestCtx
}

func (c *DashboardTestCtx) validateOperandsDynamicallyWatchedResources(t *testing.T) {
	g := c.NewWithT(t)

	newPt := xid.New().String()
	oldPt := ""

	g.Update(
		gvk.OdhApplication,
		types.NamespacedName{Name: "jupyter", Namespace: c.ApplicationNamespace},
		func(obj *unstructured.Unstructured) error {
			oldPt = resources.SetAnnotation(obj, annotations.PlatformType, newPt)
			return nil
		},
	).Eventually().Should(
		Succeed(),
	)

	g.List(
		gvk.OdhApplication,
		client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(gvk.Dashboard.Kind)},
	).Eventually().Should(And(
		HaveEach(
			jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, oldPt),
		),
	))
}

func (c *DashboardTestCtx) validateCRDReinstated(t *testing.T) {
	crds := []string{
		"acceleratorprofiles.dashboard.opendatahub.io",
		"hardwareprofiles.dashboard.opendatahub.io",
		"odhapplications.dashboard.opendatahub.io",
		"odhdocuments.dashboard.opendatahub.io",
	}

	for _, crd := range crds {
		t.Run(crd, func(t *testing.T) {
			c.ValidateCRDReinstated(t, crd)
		})
	}
}
