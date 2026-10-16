package aipipelines_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aipipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/bases/components.platform.opendatahub.io_aipipelines.yaml"

// aipipelinesSuite drives the AI Pipelines projection cases through the
// shared runner; each case is one method registered as a t.Run, building its
// DSC context explicitly with a Managed DSC-level AIPipelines selection
// (which must not leak into the module CR spec) and the Argo workflows
// controllers config under test.
type aipipelinesSuite struct {
	*projection.Runner
}

func (s aipipelinesSuite) nilArgoConfigProjectsManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.argoWorkflowsControllers.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aipipelinesSuite) presentArgoConfigWithEmptyStateProjectsManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIPipelines.ArgoWorkflowsControllers = &componentApi.ArgoWorkflowsControllersSpec{}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.argoWorkflowsControllers.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aipipelinesSuite) explicitManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIPipelines.ArgoWorkflowsControllers = &componentApi.ArgoWorkflowsControllersSpec{
		ManagementState: operatorv1.Managed,
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.argoWorkflowsControllers.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aipipelinesSuite) explicitRemoved(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIPipelines.ArgoWorkflowsControllers = &componentApi.ArgoWorkflowsControllersSpec{
		ManagementState: operatorv1.Removed,
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.argoWorkflowsControllers.managementState == %q`, operatorv1.Removed),
	)))
}

// TestBuildModuleCRProjection validates AI Pipelines projections against the
// real module CRD from the local downloaded assets. It migrates the former
// TestBuildModuleCR and TestBuildModuleCRDefaultsArgoToManaged coverage.
func TestBuildModuleCRProjection(t *testing.T) {
	h := aipipelines.NewHandler()
	s := aipipelinesSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("nil argo config projects managed", s.nilArgoConfigProjectsManaged)
	t.Run("present argo config with empty state projects managed", s.presentArgoConfigWithEmptyStateProjectsManaged)
	t.Run("explicit managed", s.explicitManaged)
	t.Run("explicit removed", s.explicitRemoved)
}
