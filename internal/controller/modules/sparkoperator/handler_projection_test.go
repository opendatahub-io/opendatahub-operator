package sparkoperator_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/sparkoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/components.platform.opendatahub.io_sparkoperators.yaml"

// sparkOperatorSuite drives the Spark Operator projection cases through the
// shared runner; each case is one method registered as a t.Run, building its
// DSC context explicitly from newDSC. The common spec has no fields,
// so there is no populated spec variant: the projection always emits an
// empty spec. Each case asserts the DSC-level managementState is not
// projected into the module CR (ported from main's BasicProjection, where
// the CRD still accepts the field so admission alone cannot catch it), and
// strict admission rejects any other field the handler might wrongly emit.
type sparkOperatorSuite struct {
	*projection.Runner
}

func (s sparkOperatorSuite) minimalDSCWithEmptyManagementState(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.SparkOperator.ManagementState = ""

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(jq.Match(`.spec.managementState == null`)))
}

func (s sparkOperatorSuite) managedDSC(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.SparkOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(jq.Match(`.spec.managementState == null`)))
}

func (s sparkOperatorSuite) removedDSC(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.SparkOperator.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(jq.Match(`.spec.managementState == null`)))
}

// TestBuildModuleCRProjection validates the Spark Operator projection against
// the real module CRD from the local downloaded assets.
func TestBuildModuleCRProjection(t *testing.T) {
	h := sparkoperator.NewHandler()
	s := sparkOperatorSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("minimal dsc with empty management state", s.minimalDSCWithEmptyManagementState)
	t.Run("managed dsc", s.managedDSC)
	t.Run("removed dsc", s.removedDSC)
}
