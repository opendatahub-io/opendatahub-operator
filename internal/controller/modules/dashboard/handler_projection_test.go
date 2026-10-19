package dashboard_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crds/components.platform.opendatahub.io_dashboards.yaml"

// dashboardSuite drives the Dashboard projection cases through the shared
// runner; each case is one method registered as a t.Run, building its DSC
// context explicitly from newDSC and amending only the fields under test.
// Since #4208 the Dashboard CR core managementState tracks the standard
// Dashboard only; the portal state is projected separately under
// spec.maasPortal and the compound OR lives in PopulatePlatformModule
// (covered in handler_test.go).
type dashboardSuite struct {
	*projection.Runner
}

func (s dashboardSuite) standardManagedWithGatewayAndComponentReferences(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{GatewayConfig: &serviceApi.GatewayConfig{
			Status: serviceApi.GatewayConfigStatus{Domain: "dashboard.example.com"},
		}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.gateway.domain == "dashboard.example.com"`),
		jq.Match(`.spec.components | length == 4`),
		jq.Match(`.spec.components.modelregistry.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.components.mlflowoperator.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.components.trustyai.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.components.aipipelines.managementState == %q`, operatorv1.Managed),
	)))
}

func (s dashboardSuite) gatewayOmittedWhenDomainEmpty(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.gateway == null`),
	)))
}

func (s dashboardSuite) emptyManagementStateProjectsRemoved(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.Standard.ManagementState = ""

	// The handler normalizes an empty standard state to Removed (#4208);
	// an unset portal state also projects Removed.
	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.components.modelregistry.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.components.aipipelines.managementState == %q`, operatorv1.Managed),
	)))
}

func (s dashboardSuite) componentReferencesDefaultToRemovedWhenEmpty(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""
	dsc.Spec.Components.AIPipelines.ManagementState = ""

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.components.modelregistry.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.components.aipipelines.managementState == %q`, operatorv1.Removed),
	)))
}

func (s dashboardSuite) notebooksNamespaceFallsBackToRHOAIDefault(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: ""},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.notebooksNamespace == %q`, cluster.DefaultNotebooksNamespaceRHOAI),
		jq.Match(`.spec.modelRegistryNamespace == null`),
	)))
}

func (s dashboardSuite) notebooksNamespaceFallsBackToODHDefault(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: ""},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.OpenDataHub}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.notebooksNamespace == %q`, cluster.DefaultNotebooksNamespaceODH),
	)))
}

func (s dashboardSuite) customNotebooksNamespace(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: "custom-notebooks"},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: ""},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.notebooksNamespace == "custom-notebooks"`),
	)))
}

func (s dashboardSuite) notebooksNamespaceOmittedWhenWorkbenchesNotManaged(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Removed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: ""},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.notebooksNamespace == null`),
	)))
}

func (s dashboardSuite) modelRegistryNamespaceProjectedFromAIHub(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Removed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Managed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: "rhoai-model-registries"},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelRegistryNamespace == "rhoai-model-registries"`),
		jq.Match(`.spec.notebooksNamespace == null`),
	)))
}

func (s dashboardSuite) modelRegistryNamespaceOmittedWhenAIHubNotManaged(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Removed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: "rhoai-model-registries"},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelRegistryNamespace == null`),
	)))
}

func (s dashboardSuite) modelRegistryNamespaceOmittedWhenEmpty(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Removed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Managed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: ""},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelRegistryNamespace == null`),
	)))
}

func (s dashboardSuite) notebooksNamespaceFallsBackToRHOAIDefaultOnManagedRhoai(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: ""},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.ManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.notebooksNamespace == %q`, cluster.DefaultNotebooksNamespaceRHOAI),
	)))
}

func (s dashboardSuite) bothNamespacesProjected(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: "rhods-notebooks"},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Managed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: "rhoai-model-registries"},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.notebooksNamespace == "rhods-notebooks"`),
		jq.Match(`.spec.modelRegistryNamespace == "rhoai-model-registries"`),
	)))
}

func (s dashboardSuite) nilCfgNotebooksFallbackDefaultsToODH(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec:        common.ManagementSpec{ManagementState: operatorv1.Managed},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{WorkbenchNamespace: ""},
	}
	dsc.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec:  common.ManagementSpec{ManagementState: operatorv1.Removed},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: ""},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.notebooksNamespace == %q`, cluster.DefaultNotebooksNamespaceODH),
	)))
}

func (s dashboardSuite) maasPortalProjectedManaged(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{GatewayConfig: &serviceApi.GatewayConfig{
			Status: serviceApi.GatewayConfigStatus{Domain: "dashboard.example.com"},
		}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Managed),
		// No legacy maasConsumerPortal key (renamed to maasPortal in #4208).
		jq.Match(`.spec.maasConsumerPortal == null`),
	)))
}

func (s dashboardSuite) maasPortalProjectedRemoved(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{GatewayConfig: &serviceApi.GatewayConfig{
			Status: serviceApi.GatewayConfigStatus{Domain: "dashboard.example.com"},
		}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.maasConsumerPortal == null`),
	)))
}

func (s dashboardSuite) effectiveStateBothManaged(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Managed),
	)))
}

func (s dashboardSuite) effectiveStateStandardOnly(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Removed),
	)))
}

// effectiveStatePortalOnly documents the #4208 decoupling: the CR core
// managementState tracks the standard Dashboard only, so a portal-only
// deployment projects Removed at the CR level while the operator stays up
// via the compound OR in PopulatePlatformModule (see handler_test.go).
func (s dashboardSuite) effectiveStatePortalOnly(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Managed),
	)))
}

func (s dashboardSuite) effectiveStatePortalWithAbsentStandard(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.Standard.ManagementState = ""
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Managed),
	)))
}

func (s dashboardSuite) effectiveStateBothRemoved(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.maasPortal.managementState == %q`, operatorv1.Removed),
	)))
}

// TestBuildModuleCRProjection validates Dashboard projections against the real
// chart CRD from the local downloaded assets. It migrates every successful
// BuildModuleCR case from handler_test.go. Admission (dry-run, strict) is the
// validation: unknown fields are rejected instead of pruned. Value checks run
// on the admitted response only, and assert only what the handler projected —
// not values the CRD schema defaults on top. The CRD CEL rule requires
// metadata.name == default-dashboard.
func TestBuildModuleCRProjection(t *testing.T) {
	h := dashboard.NewHandler()
	s := dashboardSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("standard managed with gateway and component references", s.standardManagedWithGatewayAndComponentReferences)
	t.Run("gateway omitted when domain empty", s.gatewayOmittedWhenDomainEmpty)
	t.Run("empty management state projects removed", s.emptyManagementStateProjectsRemoved)
	t.Run("component references default to removed when empty", s.componentReferencesDefaultToRemovedWhenEmpty)
	t.Run("notebooks namespace falls back to RHOAI default", s.notebooksNamespaceFallsBackToRHOAIDefault)
	t.Run("notebooks namespace falls back to ODH default", s.notebooksNamespaceFallsBackToODHDefault)
	t.Run("custom notebooks namespace", s.customNotebooksNamespace)
	t.Run("notebooks namespace omitted when workbenches not managed", s.notebooksNamespaceOmittedWhenWorkbenchesNotManaged)
	t.Run("model registry namespace projected from AI Hub", s.modelRegistryNamespaceProjectedFromAIHub)
	t.Run("model registry namespace omitted when AI Hub not managed", s.modelRegistryNamespaceOmittedWhenAIHubNotManaged)
	t.Run("model registry namespace omitted when empty", s.modelRegistryNamespaceOmittedWhenEmpty)
	t.Run("notebooks namespace falls back to RHOAI default on ManagedRhoai", s.notebooksNamespaceFallsBackToRHOAIDefaultOnManagedRhoai)
	t.Run("both namespaces projected", s.bothNamespacesProjected)
	t.Run("nil cfg notebooks fallback defaults to ODH", s.nilCfgNotebooksFallbackDefaultsToODH)
	t.Run("maas portal projected managed", s.maasPortalProjectedManaged)
	t.Run("maas portal projected removed", s.maasPortalProjectedRemoved)
	t.Run("effective state both managed", s.effectiveStateBothManaged)
	t.Run("effective state standard only", s.effectiveStateStandardOnly)
	t.Run("effective state portal only", s.effectiveStatePortalOnly)
	t.Run("effective state portal with absent standard", s.effectiveStatePortalWithAbsentStandard)
	t.Run("effective state both removed", s.effectiveStateBothRemoved)
}
