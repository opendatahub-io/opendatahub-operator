//nolint:testpackage // Reuses handler constants asserted by the migrated tests.
package mlflowoperator

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/bases/components.platform.opendatahub.io_mlflowoperators.yaml"

const mlflowTestDSCName = "test-dsc"

// mlflowOperatorSuite drives the MLflow projection cases through the shared runner;
// each case is one method registered as a t.Run, building its DSC context
// explicitly with the MLflow selection under test. The applications
// namespace is injected into the operator Deployment elsewhere and is
// unknown to the module schema, so any leak is rejected by strict admission.
type mlflowOperatorSuite struct {
	*projection.Runner
}

func (s mlflowOperatorSuite) nilConfigMinimalOutput(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.gatewayName == "data-science-gateway"`),
		jq.Match(`.spec.sectionTitle == null`),
		jq.Match(`.spec.gateway == null`),
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func (s mlflowOperatorSuite) configWithoutGatewayDomain(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.SelfManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.gatewayName == "data-science-gateway"`),
		jq.Match(`.spec.sectionTitle == "OpenShift Self Managed Services"`),
		jq.Match(`.spec.gateway == null`),
	)))
}

func (s mlflowOperatorSuite) configWithGatewayDomain(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{
			GatewayConfig: &serviceApi.GatewayConfig{
				Status: serviceApi.GatewayConfigStatus{Domain: mlflowTestGatewayURL},
			},
			Release: common.Release{Name: cluster.SelfManagedRhoai},
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.gatewayName == "data-science-gateway"`),
		jq.Match(`.spec.sectionTitle == "OpenShift Self Managed Services"`),
		jq.Match(`.spec.gateway.domain == %q`, mlflowTestGatewayURL),
	)))
}

func (s mlflowOperatorSuite) odhPlatformTitle(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.OpenDataHub}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.sectionTitle == "OpenShift Open Data Hub"`),
	)))
}

func (s mlflowOperatorSuite) managedRhoaiPlatformTitle(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: cluster.ManagedRhoai}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.sectionTitle == "OpenShift Self Managed Services"`),
		// The management state annotation is set from the DSC even with a
		// fully configured platform-mode input (ported from main's
		// TestBuildModuleCR_PlatformMode).
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func (s mlflowOperatorSuite) unknownPlatformTitleFallback(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
		Cfg: &modules.ModuleCRConfig{Release: common.Release{Name: "unknown-platform"}},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.sectionTitle == "MLflow"`),
	)))
}

func (s mlflowOperatorSuite) removedStateAnnotation(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Removed),
	)))
}

func (s mlflowOperatorSuite) emptyStateNormalizedToRemoved(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName}}
	dsc.Spec.Components.MLflowOperator.ManagementState = ""

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.metadata.annotations[%q] == %q`, annotations.ManagementStateAnnotation, operatorv1.Removed),
	)))
}

// TestBuildModuleCRProjection validates MLflow projections against the real
// module CRD from the local downloaded assets. It consolidates the successful
// BuildModuleCR cases and the two former offline schema-validation scenarios
// (with and without a gateway domain) from handler_test.go, replacing the
// vendored-schema loader with real API-server admission.
func TestBuildModuleCRProjection(t *testing.T) {
	h := NewHandler()
	s := mlflowOperatorSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("nil config minimal output", s.nilConfigMinimalOutput)
	t.Run("config without gateway domain", s.configWithoutGatewayDomain)
	t.Run("config with gateway domain", s.configWithGatewayDomain)
	t.Run("odh platform title", s.odhPlatformTitle)
	t.Run("managed rhoai platform title", s.managedRhoaiPlatformTitle)
	t.Run("unknown platform title fallback", s.unknownPlatformTitleFallback)
	t.Run("removed state annotation", s.removedStateAnnotation)
	t.Run("empty state normalized to removed", s.emptyStateNormalizedToRemoved)
}
