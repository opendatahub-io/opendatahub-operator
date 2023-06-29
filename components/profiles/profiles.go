package profiles

import (
	dsc "github.com/opendatahub-io/opendatahub-operator/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/components/dashboard"
)

type ReconciliationPlan struct {
	Components map[string]bool
}

type ProfileConfig struct {
	ComponentDefaults map[string]bool
}

var profileConfigs = map[string]ProfileConfig{
	dsc.ProfileServing: {
		ComponentDefaults: map[string]bool{
			"serving":               true,
			"training":              false,
			"workbenches":           false,
			dashboard.ComponentName: true,
		},
	},
	dsc.ProfileTraining: {
		ComponentDefaults: map[string]bool{
			"serving":               false,
			"training":              true,
			"workbenches":           false,
			dashboard.ComponentName: true,
		},
	},
	dsc.ProfileWorkbench: {
		ComponentDefaults: map[string]bool{
			"serving":               false,
			"training":              false,
			"workbenches":           true,
			dashboard.ComponentName: true,
		},
	},
	dsc.ProfileFull: {
		ComponentDefaults: map[string]bool{
			"serving":               true,
			"training":              true,
			"workbenches":           true,
			dashboard.ComponentName: true,
		},
	},
	// Add more profiles and their component defaults as needed
}

func CreateReconciliationPlan(instance *dsc.DataScienceCluster) *ReconciliationPlan {
	plan := &ReconciliationPlan{}

	profile := instance.Spec.Profile
	if profile == "" {
		profile = dsc.ProfileFull
	}

	setServingProfile(profileConfigs, plan, instance)
	// Similarly set other profiles
	return plan
}

func setServingProfile(profiledefaults map[string]ProfileConfig, plan *ReconciliationPlan, instance *dsc.DataScienceCluster) {

	// serving is enabled by default, unless explicitly overriden
	plan.Components["serving"] = profiledefaults[dsc.ProfileServing].ComponentDefaults["serving"] || *instance.Spec.Components.Serving.Enabled
	// training is disabled by default, unless explicitly overriden
	plan.Components["training"] = profiledefaults[dsc.ProfileServing].ComponentDefaults["serving"] && *instance.Spec.Components.Training.Enabled
	// workbenches is disabled by default, unless explicitly overriden
	plan.Components["workbenches"] = profiledefaults[dsc.ProfileServing].ComponentDefaults["serving"] && *instance.Spec.Components.Workbenches.Enabled
	// dashboard is enabled by default, unless explicitly overriden
	plan.Components[dashboard.ComponentName] = profiledefaults[dsc.ProfileServing].ComponentDefaults[dashboard.ComponentName] || *instance.Spec.Components.Dashboard.Enabled
}
