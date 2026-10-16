//nolint:testpackage // Reuses handler fixtures shared with handler_test.go.
package trainer

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/bases/components.platform.opendatahub.io_trainers.yaml"

// trainerSuite drives the Trainer projection cases through the shared
// runner; each case is one method registered as a t.Run, building its DSC
// context explicitly from newDSC. The common spec has no fields; the
// module CR surface is the projected appNamespace. The trainers schema has
// no defaults and no managementState key, so DSC-level state leakage is
// rejected by strict admission and the admitted spec reflects the projection
// exactly.
type trainerSuite struct {
	*projection.Runner
}

func (s trainerSuite) populatedAppNamespace(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{ApplicationsNamespace: testAppsNS},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.appNamespace == %q`, testAppsNS),
	)))
}

func (s trainerSuite) emptyAppNamespace(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.appNamespace == ""`),
	)))
}

func (s trainerSuite) nilConfigMinimalOutput(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec == {}`),
	)))
}

// TestBuildModuleCRProjection validates Trainer projections against the real
// module CRD from the local downloaded assets.
func TestBuildModuleCRProjection(t *testing.T) {
	h := NewHandler()
	s := trainerSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("populated app namespace", s.populatedAppNamespace)
	t.Run("empty app namespace", s.emptyAppNamespace)
	t.Run("nil config minimal output", s.nilConfigMinimalOutput)
}
