//nolint:testpackage // Reuses handler fixtures shared with handler_test.go.
package workbenches

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/workbenches.crd.yaml"

// workbenchesSuite drives the Workbenches projection cases through the shared
// runner; each case is one method registered as a t.Run, building its DSC
// context explicitly from newDSC and amending only the fields under test.
// BuildModuleCR always emits managementState and mlflowEnabled explicitly and
// derives gatewayDomain and platform from the module CR config. Both RHOAI
// release identities map to the module enum SelfManagedRhoai; unknown
// releases fall back to OpenDataHub. The deprecated notebooksV1 stanza is
// unknown to the module schema, so any leak is rejected by strict admission.
type workbenchesSuite struct {
	*projection.Runner
}

func (s workbenchesSuite) minimalManagedSelectionWithNilConfig(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.mlflowEnabled == false`),
		jq.Match(`.spec.workbenchNamespace == "my-workbenches"`),
		jq.Match(`.spec.workbenchesV2.managementState == %q`, operatorv1.Removed),
		// Orchestrator-only fields stay unprojected without a config.
		jq.Match(`.spec.gatewayDomain == null`),
		jq.Match(`.spec.platform == null`),
	))
}

func (s workbenchesSuite) populatedConfigWithGateway(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.mlflowEnabled == false`),
		jq.Match(`.spec.workbenchNamespace == "my-workbenches"`),
		jq.Match(`.spec.workbenchesV2.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.gatewayDomain == "apps.example.com"`),
		jq.Match(`.spec.platform == "OpenDataHub"`),
		jq.Match(`.spec.notebooksV1 == null`),
	))
}

func (s workbenchesSuite) mlflowEnabled(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, And(
		jq.Match(`.spec.mlflowEnabled == true`),
	))
}

func (s workbenchesSuite) workbenchesV2Managed(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches.WorkbenchesV2.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, And(
		jq.Match(`.spec.workbenchesV2.managementState == %q`, operatorv1.Managed),
	))
}

func (s workbenchesSuite) selfManagedRhoaiPlatformMapping(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			GatewayDomain: "apps.example.com",
			Release:       common.Release{Name: cluster.SelfManagedRhoai},
		},
	}, And(
		jq.Match(`.spec.platform == "SelfManagedRhoai"`),
	))
}

func (s workbenchesSuite) managedRhoaiPlatformMapping(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			GatewayDomain: "apps.example.com",
			Release:       common.Release{Name: cluster.ManagedRhoai},
		},
	}, And(
		jq.Match(`.spec.platform == "SelfManagedRhoai"`),
	))
}

func (s workbenchesSuite) unknownReleaseFallsBackToODH(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			GatewayDomain: "apps.example.com",
			Release:       common.Release{Name: "unknown-platform"},
		},
	}, And(
		jq.Match(`.spec.platform == "OpenDataHub"`),
	))
}

func (s workbenchesSuite) removedSelection(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Removed),
	))
}

// TestBuildModuleCRProjection validates Workbenches projections against the
// real chart CRD from the local downloaded assets. It consolidates the
// successful BuildModuleCR cases and the two former offline schema-validation
// scenarios (which shared identical inputs) from handler_test.go, replacing
// the vendored-schema loader with real API-server admission.
func TestBuildModuleCRProjection(t *testing.T) {
	h := NewHandler()
	s := workbenchesSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("minimal managed selection with nil config", s.minimalManagedSelectionWithNilConfig)
	t.Run("populated config with gateway", s.populatedConfigWithGateway)
	t.Run("mlflow enabled", s.mlflowEnabled)
	t.Run("workbenches v2 managed", s.workbenchesV2Managed)
	t.Run("self managed rhoai platform mapping", s.selfManagedRhoaiPlatformMapping)
	t.Run("managed rhoai platform mapping", s.managedRhoaiPlatformMapping)
	t.Run("unknown release falls back to odh", s.unknownReleaseFallsBackToODH)
	t.Run("removed selection", s.removedSelection)
}
