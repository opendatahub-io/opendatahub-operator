package aigateway_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/bases/components.platform.opendatahub.io_aigateways.yaml"

// aigatewaySuite drives the AI Gateway projection cases through the shared
// runner; each case is one method registered as a t.Run, building its DSC
// input explicitly from newDSC (the DSC-level AIGateway selection) and
// amending the kserve state and the submodule states under test. The
// matrix methods preserve the state matrix of the former
// TestCanonicalModelsAsAService BuildModuleCR portion: modelsAsAService x
// DSC AIGateway state x kserve state, with the batch gateway and the grid
// submodule Managed. The
// mixed methods cover the remaining batch-gateway combinations, including
// the inverse of the matrix. An empty projected submodule value means the
// handler emits an empty object, which must survive admission as a key (its
// defaulted contents are not asserted). Any kserve or DSC-level field leaking
// into the projection is unknown to the CRD and rejected by strict
// admission, so admission itself guards the isolation.
type aigatewaySuite struct {
	*projection.Runner
}

func (s aigatewaySuite) maasEmptyAigatewayEmptyKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayEmptyKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayEmptyKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayManagedKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayManagedKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayManagedKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayRemovedKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayRemovedKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyAigatewayRemovedKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayEmptyKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayEmptyKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayEmptyKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayManagedKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayManagedKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayManagedKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayRemovedKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayRemovedKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedAigatewayRemovedKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayEmptyKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayEmptyKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayEmptyKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = ""
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayManagedKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayManagedKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayManagedKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayRemovedKserveEmptyBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = ""
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayRemovedKserveManagedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedAigatewayRemovedKserveRemovedBatchManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyBatchEmptyAigatewayManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = ""
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway != null`),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasEmptyBatchRemovedAigatewayManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = ""
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService != null`),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedBatchEmptyAigatewayManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = ""
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway != null`),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasManagedBatchRemovedAigatewayManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedBatchEmptyAigatewayManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = ""
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway != null`),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

func (s aigatewaySuite) maasRemovedBatchRemovedAigatewayManaged(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Removed
	dsc.Spec.Components.AIGateway.AIGrid.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelsAsAService.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.batchGateway.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.aiGrid.managementState == %q`, operatorv1.Managed),
	)))
}

// basicProjectionInputs ports main's TestBuildModuleCR_BasicProjection: the
// gateway Managed with every other selection left empty. The matrix and
// mixed cases always pin either the batch gateway (Managed) or KServe
// (Removed), so this input combination is unique to this case. Main also
// asserted managementState is not projected; the CRD accepts the field, so
// only a pre-admission check can catch a leak.
func (s aigatewaySuite) basicProjectionInputs(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.managementState == null`),
	)))
}

// TestBuildModuleCRProjection validates the AI Gateway projection against the
// real module CRD from the local downloaded assets.
func TestBuildModuleCRProjection(t *testing.T) {
	h := aigateway.NewHandler()
	s := aigatewaySuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("maas=empty/aigateway=empty/kserve=empty/batch=managed", s.maasEmptyAigatewayEmptyKserveEmptyBatchManaged)
	t.Run("maas=empty/aigateway=empty/kserve=managed/batch=managed", s.maasEmptyAigatewayEmptyKserveManagedBatchManaged)
	t.Run("maas=empty/aigateway=empty/kserve=removed/batch=managed", s.maasEmptyAigatewayEmptyKserveRemovedBatchManaged)
	t.Run("maas=empty/aigateway=managed/kserve=empty/batch=managed", s.maasEmptyAigatewayManagedKserveEmptyBatchManaged)
	t.Run("maas=empty/aigateway=managed/kserve=managed/batch=managed", s.maasEmptyAigatewayManagedKserveManagedBatchManaged)
	t.Run("maas=empty/aigateway=managed/kserve=removed/batch=managed", s.maasEmptyAigatewayManagedKserveRemovedBatchManaged)
	t.Run("maas=empty/aigateway=removed/kserve=empty/batch=managed", s.maasEmptyAigatewayRemovedKserveEmptyBatchManaged)
	t.Run("maas=empty/aigateway=removed/kserve=managed/batch=managed", s.maasEmptyAigatewayRemovedKserveManagedBatchManaged)
	t.Run("maas=empty/aigateway=removed/kserve=removed/batch=managed", s.maasEmptyAigatewayRemovedKserveRemovedBatchManaged)
	t.Run("maas=managed/aigateway=empty/kserve=empty/batch=managed", s.maasManagedAigatewayEmptyKserveEmptyBatchManaged)
	t.Run("maas=managed/aigateway=empty/kserve=managed/batch=managed", s.maasManagedAigatewayEmptyKserveManagedBatchManaged)
	t.Run("maas=managed/aigateway=empty/kserve=removed/batch=managed", s.maasManagedAigatewayEmptyKserveRemovedBatchManaged)
	t.Run("maas=managed/aigateway=managed/kserve=empty/batch=managed", s.maasManagedAigatewayManagedKserveEmptyBatchManaged)
	t.Run("maas=managed/aigateway=managed/kserve=managed/batch=managed", s.maasManagedAigatewayManagedKserveManagedBatchManaged)
	t.Run("maas=managed/aigateway=managed/kserve=removed/batch=managed", s.maasManagedAigatewayManagedKserveRemovedBatchManaged)
	t.Run("maas=managed/aigateway=removed/kserve=empty/batch=managed", s.maasManagedAigatewayRemovedKserveEmptyBatchManaged)
	t.Run("maas=managed/aigateway=removed/kserve=managed/batch=managed", s.maasManagedAigatewayRemovedKserveManagedBatchManaged)
	t.Run("maas=managed/aigateway=removed/kserve=removed/batch=managed", s.maasManagedAigatewayRemovedKserveRemovedBatchManaged)
	t.Run("maas=removed/aigateway=empty/kserve=empty/batch=managed", s.maasRemovedAigatewayEmptyKserveEmptyBatchManaged)
	t.Run("maas=removed/aigateway=empty/kserve=managed/batch=managed", s.maasRemovedAigatewayEmptyKserveManagedBatchManaged)
	t.Run("maas=removed/aigateway=empty/kserve=removed/batch=managed", s.maasRemovedAigatewayEmptyKserveRemovedBatchManaged)
	t.Run("maas=removed/aigateway=managed/kserve=empty/batch=managed", s.maasRemovedAigatewayManagedKserveEmptyBatchManaged)
	t.Run("maas=removed/aigateway=managed/kserve=managed/batch=managed", s.maasRemovedAigatewayManagedKserveManagedBatchManaged)
	t.Run("maas=removed/aigateway=managed/kserve=removed/batch=managed", s.maasRemovedAigatewayManagedKserveRemovedBatchManaged)
	t.Run("maas=removed/aigateway=removed/kserve=empty/batch=managed", s.maasRemovedAigatewayRemovedKserveEmptyBatchManaged)
	t.Run("maas=removed/aigateway=removed/kserve=managed/batch=managed", s.maasRemovedAigatewayRemovedKserveManagedBatchManaged)
	t.Run("maas=removed/aigateway=removed/kserve=removed/batch=managed", s.maasRemovedAigatewayRemovedKserveRemovedBatchManaged)
	t.Run("maas=empty/batch=empty/aigateway=managed", s.maasEmptyBatchEmptyAigatewayManaged)
	t.Run("maas=empty/batch=removed/aigateway=managed", s.maasEmptyBatchRemovedAigatewayManaged)
	t.Run("maas=managed/batch=empty/aigateway=managed", s.maasManagedBatchEmptyAigatewayManaged)
	t.Run("maas=managed/batch=removed/aigateway=managed", s.maasManagedBatchRemovedAigatewayManaged)
	t.Run("maas=removed/batch=empty/aigateway=managed", s.maasRemovedBatchEmptyAigatewayManaged)
	t.Run("maas=removed/batch=removed/aigateway=managed", s.maasRemovedBatchRemovedAigatewayManaged)
	t.Run("basic projection inputs (all other selections empty)", s.basicProjectionInputs)
}
