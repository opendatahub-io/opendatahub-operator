package aihub_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aihub"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "overlays/aihub/components.platform.opendatahub.io_aihubs.yaml"

// aihubSuite drives the AI Hub projection cases through the shared runner;
// each case is one method registered as a t.Run, building its DSC context
// explicitly from newDSC and amending only the fields under test. The CRD
// requires nonempty spec.applicationNamespace and spec.instancesNamespace,
// so the off-contract nil-cfg behavior tests remain in handler_test.go and
// are not admitted here. Since #4197 the handler projects
// spec.instancesNamespace verbatim: the default is applied by the DSC v3
// defaulting webhook, so cases simulate it by setting the spec field.
type aihubSuite struct {
	*projection.Runner
}

func (s aihubSuite) webhookDefaultedInstancesNamespace(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = componentApi.DefaultModelRegistriesNamespace

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{ApplicationsNamespace: "test-apps-ns"},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationNamespace == "test-apps-ns"`),
		jq.Match(`.spec.instancesNamespace == %q`, componentApi.DefaultModelRegistriesNamespace),
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func (s aihubSuite) customInstancesNamespaceWithGatewayDomain(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = "my-registries"

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			ApplicationsNamespace: "test-apps-ns",
			GatewayConfig: &serviceApi.GatewayConfig{
				Status: serviceApi.GatewayConfigStatus{Domain: "apps.example.com"},
			},
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationNamespace == "test-apps-ns"`),
		jq.Match(`.spec.instancesNamespace == "my-registries"`),
		jq.Match(`.spec.gateway.domain == "apps.example.com"`),
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func (s aihubSuite) omittedGateway(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = componentApi.DefaultModelRegistriesNamespace

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			ApplicationsNamespace: "test-apps-ns",
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationNamespace == "test-apps-ns"`),
		jq.Match(`.spec.instancesNamespace == %q`, componentApi.DefaultModelRegistriesNamespace),
		jq.Match(`.spec.gateway == null`),
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func (s aihubSuite) emptyManagementStateNormalizedToRemoved(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""
	dsc.Spec.Components.AIHub.InstancesNamespace = componentApi.DefaultModelRegistriesNamespace

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{ApplicationsNamespace: "test-apps-ns"},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationNamespace == "test-apps-ns"`),
		jq.Match(`.spec.instancesNamespace == %q`, componentApi.DefaultModelRegistriesNamespace),
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Removed),
	)))
}

// instancesNamespaceMayEqualApplicationsNamespace admits the case where the
// instances namespace equals the applications namespace: the module CRD
// explicitly allows it (see the instancesNamespace description), so the
// strict dry-run create must succeed.
func (s aihubSuite) instancesNamespaceMayEqualApplicationsNamespace(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = "test-apps-ns"

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{ApplicationsNamespace: "test-apps-ns"},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.applicationNamespace == "test-apps-ns"`),
		jq.Match(`.spec.instancesNamespace == "test-apps-ns"`),
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

// customInstancesNamespaceWithoutGateway ports the no-gateway half of main's
// TestBuildModuleCR_BasicProjection: a custom instances namespace with no
// gateway domain configured, so the gateway stanza stays absent.
func (s aihubSuite) customInstancesNamespaceWithoutGateway(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = "my-registries"

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{ApplicationsNamespace: "test-apps-ns"},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.instancesNamespace == "my-registries"`),
		jq.Match(`.spec.gateway == null`),
	)))
}

// TestBuildModuleCRProjection validates AI Hub projections against the real
// module CRD from the local downloaded assets.
func TestBuildModuleCRProjection(t *testing.T) {
	h := aihub.NewHandler()
	s := aihubSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("webhook defaulted instances namespace", s.webhookDefaultedInstancesNamespace)
	t.Run("custom instances namespace with gateway domain", s.customInstancesNamespaceWithGatewayDomain)
	t.Run("custom instances namespace without gateway", s.customInstancesNamespaceWithoutGateway)
	t.Run("omitted gateway", s.omittedGateway)
	t.Run("empty management state normalized to removed", s.emptyManagementStateNormalizedToRemoved)
	t.Run("instances namespace may equal applications namespace", s.instancesNamespaceMayEqualApplicationsNamespace)
}
