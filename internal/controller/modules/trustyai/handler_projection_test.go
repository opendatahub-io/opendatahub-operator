package trustyai_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/bases/components.platform.opendatahub.io_trustyais.yaml"

// trustyAISuite drives the TrustyAI projection cases through the shared
// runner; each case is one method registered as a t.Run, building its DSC
// context explicitly from newDSC and amending only the fields under
// test. The permit fields are the "allow"/"deny" string enum on both the
// DSC-facing spec and the module CRD (#4210), so BuildModuleCR passes them
// through verbatim; the mcpGuardrailsMode boolean is forwarded verbatim too.
// Unset fields are omitted (omitempty) so the module CRD defaults (deny)
// apply instead of being projected as empty strings, which the enum rejects.
// DSC-level managementState is a module-schema field with its own default
// that the handler never emits; its defaulted admitted value is not asserted.
type trustyAISuite struct {
	*projection.Runner
}

func (s trustyAISuite) minimalPermitDefaults(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		// The eval.lmeval container is always projected (struct fields are
		// not omitempty), while its unset leaves are omitted so the module
		// CRD defaults (deny) apply instead of being projected as empty
		// strings, which the enum rejects.
		jq.Match(`.spec.eval.lmeval == {}`),
		jq.Match(`.spec.mcpGuardrailsMode == null`),
		jq.Match(`.spec.eval.lmeval.permitCodeExecution == null`),
		jq.Match(`.spec.eval.lmeval.permitOnline == null`),
		// managementState is a DSC-level field and must not be projected
		// into the module CR (the CRD accepts it, so admission cannot catch
		// it).
		jq.Match(`.spec.managementState == null`),
	)))
}

func (s trustyAISuite) bothPermitsAllowed(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.TrustyAI.Eval.LMEval.PermitCodeExecution = "allow"
	dsc.Spec.Components.TrustyAI.Eval.LMEval.PermitOnline = "allow"

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.eval.lmeval.permitCodeExecution == "allow"`),
		jq.Match(`.spec.eval.lmeval.permitOnline == "allow"`),
	)))
}

func (s trustyAISuite) codeExecutionAllowedOnly(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.TrustyAI.Eval.LMEval.PermitCodeExecution = "allow"
	dsc.Spec.Components.TrustyAI.Eval.LMEval.PermitOnline = "deny"

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.eval.lmeval.permitCodeExecution == "allow"`),
		jq.Match(`.spec.eval.lmeval.permitOnline == "deny"`),
	)))
}

func (s trustyAISuite) onlineAllowedOnly(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.TrustyAI.Eval.LMEval.PermitCodeExecution = "deny"
	dsc.Spec.Components.TrustyAI.Eval.LMEval.PermitOnline = "allow"

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.eval.lmeval.permitCodeExecution == "deny"`),
		jq.Match(`.spec.eval.lmeval.permitOnline == "allow"`),
	)))
}

func (s trustyAISuite) mcpGuardrailsModeEnabled(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.TrustyAI.MCPGuardrailsMode = true

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: newModuleCRConfig(),
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.mcpGuardrailsMode == true`),
		jq.Match(`.spec.eval.lmeval.permitCodeExecution == null`),
		jq.Match(`.spec.eval.lmeval.permitOnline == null`),
	)))
}

// TestBuildModuleCRProjection validates TrustyAI projections against the real
// module CRD from the local downloaded assets. It migrates the successful
// BuildModuleCR cases from handler_test.go, asserting the enum pass-through
// and the nested lmeval retention, with every projected value asserted on
// the raw pre-admission output.
func TestBuildModuleCRProjection(t *testing.T) {
	h := trustyai.NewHandler()
	s := trustyAISuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("minimal permit defaults", s.minimalPermitDefaults)
	t.Run("both permits allowed", s.bothPermitsAllowed)
	t.Run("code execution allowed only", s.codeExecutionAllowedOnly)
	t.Run("online allowed only", s.onlineAllowedOnly)
	t.Run("mcp guardrails mode enabled", s.mcpGuardrailsModeEnabled)
}
