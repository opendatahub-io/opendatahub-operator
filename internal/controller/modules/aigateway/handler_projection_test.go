package aigateway_test

import (
	"fmt"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/bases/components.platform.opendatahub.io_aigateways.yaml"

// states are the ManagementState selections every submodule dimension
// varies over: unset and both enum values.
var states = []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed}

// aigatewaySuite drives the AI Gateway projection cases through the shared
// runner. BuildModuleCR is a pure passthrough: it serializes the DSC's
// AIGatewayCommonSpec into the module CR spec. Each case is therefore one
// AIGatewayCommonSpec — assigned wholesale into the DSC — and the raw
// projection must equal it verbatim: explicit selections project exactly,
// unset selections still emit their (empty) key so the CRD can default
// them at admission, and no DSC-level field (the component's own
// managementState) leaks into the spec. Admission itself — a strict dry-run
// create against the real, unchanged CRD — guards schema conformance.
type aigatewaySuite struct {
	*projection.Runner
}

// specDim varies one submodule selection of the common spec.
type specDim func(*componentApi.AIGatewayCommonSpec, operatorv1.ManagementState)

// expand returns one spec per combination of the given dimensions' states,
// starting from the zero-value spec: each pass fans every accumulated spec
// out into one copy per state of the current dimension.
func expand(dims ...specDim) []componentApi.AIGatewayCommonSpec {
	specs := []componentApi.AIGatewayCommonSpec{{}}
	for _, dim := range dims {
		next := make([]componentApi.AIGatewayCommonSpec, 0, len(specs)*len(states))
		for _, spec := range specs {
			for _, state := range states {
				spec := spec // fresh copy per state: dims mutate in place
				dim(&spec, state)
				next = append(next, spec)
			}
		}
		specs = next
	}

	return specs
}

// stateName renders a ManagementState for subtest names.
func stateName(st operatorv1.ManagementState) string {
	if st == "" {
		return "empty"
	}
	return strings.ToLower(string(st))
}

// run validates one case. The expected spec is the case value itself run
// through the same serialization primitive the handler uses, so the
// comparison verifies everything the handler owns — the right source
// struct, no filtering, no defaulting, no leaking — and nothing about the
// serializer, which is apimachinery's business.
func (s aigatewaySuite) run(spec componentApi.AIGatewayCommonSpec) func(*testing.T) {
	return func(t *testing.T) {
		dsc := &dscApi.DataScienceCluster{}
		// The DSC-level selection is not projected, and the platform only
		// builds module CRs for enabled modules — so pin it Managed. The
		// non-empty value also keeps the leak guard honest: if the handler
		// ever serialized the whole DSCAIGateway instead of the embedded
		// common spec, managementState:"Managed" would surface in the raw
		// output and fail the equality check, whereas an empty value would
		// be omitted by omitempty and mask the leak.
		dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
		dsc.Spec.Components.AIGateway.AIGatewayCommonSpec = spec

		expected, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&spec)
		if err != nil {
			t.Fatalf("serializing expected spec: %v", err)
		}

		s.Validate(t, projection.ModuleCRSpec{
			DSCCtx: modules.DSCContext{
				DSC: dsc,
			},
		}, projection.WithPreAdmissionValidation(
			WithTransform(func(u *unstructured.Unstructured) any {
				return u.Object["spec"]
			}, Equal(expected)),
		))
	}
}

// TestBuildModuleCRProjection validates the AI Gateway projection against
// the real module CRD from the local downloaded assets. The cases are the
// full projection input space: every combination of the three submodule
// selections over unset, Managed and Removed.
func TestBuildModuleCRProjection(t *testing.T) {
	s := aigatewaySuite{Runner: projection.NewRunner(t, aigateway.NewHandler(), crdPath)}

	specs := expand(
		func(spec *componentApi.AIGatewayCommonSpec, s operatorv1.ManagementState) {
			spec.ModelsAsAService.ManagementState = s
		},
		func(spec *componentApi.AIGatewayCommonSpec, s operatorv1.ManagementState) {
			spec.BatchGateway.ManagementState = s
		},
		func(spec *componentApi.AIGatewayCommonSpec, s operatorv1.ManagementState) {
			spec.AIGrid.ManagementState = s
		},
	)

	for _, spec := range specs {
		// One subtest per submodule-state combination, e.g.
		// "maas=empty/batch=managed/grid=removed".
		name := fmt.Sprintf("maas=%s/batch=%s/grid=%s",
			stateName(spec.ModelsAsAService.ManagementState),
			stateName(spec.BatchGateway.ManagementState),
			stateName(spec.AIGrid.ManagementState))

		t.Run(name, s.run(spec))
	}
}
