package gates

import (
	"maps"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	codeflaregates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/codeflare"
	dspgates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/datasciencepipelines"
	kservegates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kserve"
	kueuegates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kueue"
	modelmeshservinggates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/modelmeshserving"
	raygates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/ray"
)

var registeredChecks = map[string]provision.UpgradeCheckFunc{
	componentApi.AIGatewayComponentName:            provision.DefaultUpgradeCheck,
	componentApi.CodeFlareComponentName:            codeflaregates.Check,
	componentApi.DashboardComponentName:            provision.DefaultUpgradeCheck,
	componentApi.DataSciencePipelinesComponentName: dspgates.Check,
	componentApi.FeastOperatorComponentName:        provision.DefaultUpgradeCheck,
	componentApi.KserveComponentName:               kservegates.Check,
	componentApi.KueueComponentName:                kueuegates.Check,
	componentApi.MCPLifecycleOperatorComponentName: provision.DefaultUpgradeCheck,
	componentApi.MLflowOperatorComponentName:       provision.DefaultUpgradeCheck,
	componentApi.ModelMeshServingComponentName:     modelmeshservinggates.Check,
	componentApi.ModelRegistryComponentName:        provision.DefaultUpgradeCheck,
	componentApi.OGXComponentName:                  provision.DefaultUpgradeCheck,
	componentApi.RayComponentName:                  raygates.Check,
	componentApi.SparkOperatorComponentName:        provision.DefaultUpgradeCheck,
	componentApi.TrainerComponentName:              provision.DefaultUpgradeCheck,
	componentApi.TrainingOperatorComponentName:     provision.DefaultUpgradeCheck,
	componentApi.TrustyAIComponentName:             provision.DefaultUpgradeCheck,
	componentApi.WorkbenchesComponentName:          provision.DefaultUpgradeCheck,
}

// Register registers every in-tree upgrade check used by auto-ack.
func Register() {
	for key, fn := range registeredChecks {
		provision.RegisterUpgradeCheck(key, fn)
	}
}

// Registered returns the upgrade-check registrations defined in this package.
func Registered() map[string]provision.UpgradeCheckFunc {
	return maps.Clone(registeredChecks)
}
