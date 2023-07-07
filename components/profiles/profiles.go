package profiles

import (
	dsc "github.com/opendatahub-io/opendatahub-operator/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/components/workbenches"
)

const (
	ProfileCore      dsc.ProfileValue = "core"
	ProfileServing   dsc.ProfileValue = "serving"
	ProfileTraining  dsc.ProfileValue = "training"
	ProfileWorkbench dsc.ProfileValue = "workbench"
)

type ProfileConfig struct {
	ComponentDefaults map[string]bool
}

var ProfileConfigs = make(map[dsc.ProfileValue]ProfileConfig)

func SetDefaultProfiles() map[dsc.ProfileValue]ProfileConfig {
	ProfileConfigs = map[dsc.ProfileValue]ProfileConfig{
		ProfileServing: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     true,
				datasciencepipelines.ComponentName: false,
				workbenches.ComponentName:          false,
				dashboard.ComponentName:            true,
			},
		},
		ProfileTraining: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     false,
				datasciencepipelines.ComponentName: true,
				workbenches.ComponentName:          false,
				dashboard.ComponentName:            true,
			},
		},
		ProfileWorkbench: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     false,
				datasciencepipelines.ComponentName: false,
				workbenches.ComponentName:          true,
				dashboard.ComponentName:            true,
			},
		},
		ProfileCore: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     true,
				datasciencepipelines.ComponentName: true,
				workbenches.ComponentName:          true,
				dashboard.ComponentName:            true,
			},
		},
		// Add more profiles and their component defaults as needed
	}

	return ProfileConfigs
}

type ReconciliationPlan struct {
	Components map[string]bool
}

func PopulatePlan(profiledefaults ProfileConfig, plan *ReconciliationPlan, instance *dsc.DataScienceCluster) {

	// serving is set to the default value, unless explicitly overriden
	plan.Components[modelmeshserving.ComponentName] = profiledefaults.ComponentDefaults[modelmeshserving.ComponentName]
	if instance.Spec.Components.ModelMeshServing.Enabled != nil {
		plan.Components[modelmeshserving.ComponentName] = *instance.Spec.Components.ModelMeshServing.Enabled
	}
	// training is set to the default value, unless explicitly overriden
	plan.Components[datasciencepipelines.ComponentName] = profiledefaults.ComponentDefaults[datasciencepipelines.ComponentName]
	if instance.Spec.Components.DataSciencePipelines.Enabled != nil {
		plan.Components[datasciencepipelines.ComponentName] = *instance.Spec.Components.DataSciencePipelines.Enabled
	}
	// workbenches is set to the default value, unless explicitly overriden
	plan.Components[workbenches.ComponentName] = profiledefaults.ComponentDefaults[workbenches.ComponentName]
	if instance.Spec.Components.Workbenches.Enabled != nil {
		plan.Components[workbenches.ComponentName] = *instance.Spec.Components.Workbenches.Enabled
	}
	// dashboard is set to the default value, unless explicitly overriden
	plan.Components[dashboard.ComponentName] = profiledefaults.ComponentDefaults[dashboard.ComponentName]
	if instance.Spec.Components.Dashboard.Enabled != nil {
		plan.Components[dashboard.ComponentName] = *instance.Spec.Components.Dashboard.Enabled
	}
}
