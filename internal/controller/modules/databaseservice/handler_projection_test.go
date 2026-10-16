package databaseservice_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/databaseservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crds/apiextensions.k8s.io_v1_customresourcedefinition.yaml"

// databaseserviceSuite drives the DatabaseService projection cases through
// the shared runner; each case is one method registered as a t.Run. The
// module has no configurable desired state: there is no populated spec
// variant, DSC management state must not project to spec.managementState,
// and defaultProvider must not be emitted — any such field is unknown to the
// empty spec schema and rejected by strict admission.
type databaseserviceSuite struct {
	*projection.Runner
}

func (s databaseserviceSuite) minimalDSCWithEmptyManagementState(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.DatabaseService.ManagementState = ""

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec == {}`),
	)))
}

func (s databaseserviceSuite) managedDSC(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec == {}`),
	)))
}

func (s databaseserviceSuite) removedDSC(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.DatabaseService.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec == {}`),
	)))
}

// TestBuildModuleCRProjection validates the intentionally empty
// DatabaseService projection against the real chart CRD from the local
// downloaded assets.
func TestBuildModuleCRProjection(t *testing.T) {
	h := databaseservice.NewHandler()
	s := databaseserviceSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("minimal dsc with empty management state", s.minimalDSCWithEmptyManagementState)
	t.Run("managed dsc", s.managedDSC)
	t.Run("removed dsc", s.removedDSC)
}
