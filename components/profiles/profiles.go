package profiles

import (
	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
)

const (
	ProfileNone      dsc.ProfileValue = "none"
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
		ProfileNone: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     false,
				datasciencepipelines.ComponentName: false,
				workbenches.ComponentName:          false,
				dashboard.ComponentName:            false,
				kserve.ComponentName:               false,
			},
		},
		ProfileServing: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     true,
				datasciencepipelines.ComponentName: false,
				workbenches.ComponentName:          false,
				dashboard.ComponentName:            true,
				kserve.ComponentName:               true,
			},
		},
		ProfileTraining: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     false,
				datasciencepipelines.ComponentName: true,
				workbenches.ComponentName:          false,
				dashboard.ComponentName:            true,
				kserve.ComponentName:               false,
			},
		},
		ProfileWorkbench: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     false,
				datasciencepipelines.ComponentName: false,
				workbenches.ComponentName:          true,
				dashboard.ComponentName:            true,
				kserve.ComponentName:               false,
			},
		},
		ProfileCore: {
			ComponentDefaults: map[string]bool{
				modelmeshserving.ComponentName:     true,
				datasciencepipelines.ComponentName: true,
				workbenches.ComponentName:          true,
				dashboard.ComponentName:            true,
				kserve.ComponentName:               true,
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

	// modelmeshserving is set to the default value, unless explicitly overriden
	plan.Components[modelmeshserving.ComponentName] = profiledefaults.ComponentDefaults[modelmeshserving.ComponentName]
	if instance.Spec.Components.ModelMeshServing.Enabled != nil {
		plan.Components[modelmeshserving.ComponentName] = *instance.Spec.Components.ModelMeshServing.Enabled
	}
	// datasciencepipelines is set to the default value, unless explicitly overriden
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
	// kserve is set to the default value, unless explicitly overriden
	plan.Components[kserve.ComponentName] = profiledefaults.ComponentDefaults[kserve.ComponentName]
	if instance.Spec.Components.Dashboard.Enabled != nil {
		plan.Components[kserve.ComponentName] = *instance.Spec.Components.Kserve.Enabled
	}
}
