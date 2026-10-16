package ray_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/bases/components.platform.opendatahub.io_rays.yaml"

// raySuite drives the Ray projection cases through the shared runner; each
// case is one method registered as a t.Run, building its DSC context
// explicitly from newDSC. A present config always projects
// applicationsNamespace, including the explicit empty string when the
// platform has no applications namespace. With a nil config the handler
// emits an empty spec, and the admitted response must keep
// applicationsNamespace absent: not projected, and not filled by a schema
// default either.
type raySuite struct {
	*projection.Runner
}

func (s raySuite) populatedApplicationsNamespace(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{ApplicationsNamespace: testAppsNS},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationsNamespace == %q`, testAppsNS),
		jq.Match(`.spec.managementState == null`),
	)))
}

func (s raySuite) emptyApplicationsNamespace(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationsNamespace == ""`),
		jq.Match(`.spec.managementState == null`),
	)))
}

func (s raySuite) nilConfigMinimalOutput(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationsNamespace == null`),
		jq.Match(`.spec.managementState == null`),
	)))
}

// TestBuildModuleCRProjection validates Ray projections against the real
// module CRD from the local downloaded assets. The CRD requires
// metadata.name == default-ray.
func TestBuildModuleCRProjection(t *testing.T) {
	h := ray.NewHandler()
	s := raySuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("populated applications namespace", s.populatedApplicationsNamespace)
	t.Run("empty applications namespace", s.emptyApplicationsNamespace)
	t.Run("nil config minimal output", s.nilConfigMinimalOutput)
}
