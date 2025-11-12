/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

const (
	LegacyDataScienceComponentName   = "data-science-pipelines-operator"
	LegacyModelRegistryComponentName = "model-registry-operator"
)

var componentNameMapping = map[string]string{
	"DataSciencePipelines": "AIPipelines",
}

// Mapping for components where v1 and v2 names differ
var componentV2ToV1NameMapping = map[string]string{
	componentApi.DataSciencePipelinesComponentName: LegacyDataScienceComponentName,
	componentApi.ModelRegistryComponentName:        LegacyModelRegistryComponentName,
	// Add others if they differ in the future
}

// convertConditions converts condition types by replacing component names.
// If v1ToV2 is true, replaces v1 names with v2 names, otherwise replaces v2 names with v1 names.
func convertConditions(conditions []common.Condition, v1ToV2 bool) []common.Condition {
	if conditions == nil {
		return nil
	}
	converted := make([]common.Condition, len(conditions))
	for i, cond := range conditions {
		converted[i] = cond
		condType := cond.Type
		// Apply all component name replacements from the mapping
		for v1Name, v2Name := range componentNameMapping {
			if v1ToV2 {
				condType = strings.ReplaceAll(condType, v1Name, v2Name)
			} else {
				condType = strings.ReplaceAll(condType, v2Name, v1Name)
			}
		}
		converted[i].Type = condType
	}
	return converted
}

// getV1ComponentName returns the v1 component name for a given v2 component name.
// Handles cases where component names differ between versions.
func getV1ComponentName(v2ComponentName string) string {
	if v1Name, exists := componentV2ToV1NameMapping[v2ComponentName]; exists {
		return v1Name
	}
	return v2ComponentName // Most components have the same name in v1 and v2
}

// constructInstalledComponentsFromV2Status constructs the InstalledComponents map for v1
// based on the component management states in v2 status
func constructInstalledComponentsFromV2Status(v2Status dscv2.DataScienceClusterStatus) map[string]bool {
	return map[string]bool{
		getV1ComponentName(componentApi.DashboardComponentName):            v2Status.Components.Dashboard.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.DataSciencePipelinesComponentName): v2Status.Components.AIPipelines.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.KserveComponentName):               v2Status.Components.Kserve.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.KueueComponentName):                v2Status.Components.Kueue.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.RayComponentName):                  v2Status.Components.Ray.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.TrustyAIComponentName):             v2Status.Components.TrustyAI.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.ModelRegistryComponentName):        v2Status.Components.ModelRegistry.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.TrainingOperatorComponentName):     v2Status.Components.TrainingOperator.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.FeastOperatorComponentName):        v2Status.Components.FeastOperator.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.LlamaStackOperatorComponentName):   v2Status.Components.LlamaStackOperator.ManagementState == operatorv1.Managed,
		getV1ComponentName(componentApi.WorkbenchesComponentName):          v2Status.Components.Workbenches.ManagementState == operatorv1.Managed,
	}
}

// ConvertTo converts this DataScienceCluster (v1) to the Hub version (v2).
func (c *DataScienceCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dscv2.DataScienceCluster)

	dst.ObjectMeta = c.ObjectMeta

	dst.Spec = dscv2.DataScienceClusterSpec{
		Components: dscv2.Components{
			Dashboard:   c.Spec.Components.Dashboard,
			Workbenches: c.Spec.Components.Workbenches,
			AIPipelines: c.Spec.Components.DataSciencePipelines,
			Kserve:      c.Spec.Components.Kserve,
			Kueue: componentApi.DSCKueue{
				KueueManagementSpec: componentApi.KueueManagementSpec{
					ManagementState: c.Spec.Components.Kueue.ManagementState,
				},
				KueueCommonSpec:       c.Spec.Components.Kueue.KueueCommonSpec,
				KueueDefaultQueueSpec: c.Spec.Components.Kueue.KueueDefaultQueueSpec,
			},
			Ray:              c.Spec.Components.Ray,
			TrustyAI:         c.Spec.Components.TrustyAI,
			ModelRegistry:    c.Spec.Components.ModelRegistry,
			TrainingOperator: c.Spec.Components.TrainingOperator,
			Trainer: componentApi.DSCTrainer{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
			},
			FeastOperator:      c.Spec.Components.FeastOperator,
			LlamaStackOperator: c.Spec.Components.LlamaStackOperator,
		},
	}

	// Convert status with field renaming: DataSciencePipelines -> AIPipelines
	// and condition type renaming: DataSciencePipelinesReady -> AIPipelinesReady
	dst.Status = dscv2.DataScienceClusterStatus{
		Status: common.Status{
			Phase:              c.Status.Phase,
			ObservedGeneration: c.Status.ObservedGeneration,
			Conditions:         convertConditions(c.Status.Conditions, true),
		},
		RelatedObjects: c.Status.RelatedObjects,
		ErrorMessage:   c.Status.ErrorMessage,
		Components: dscv2.ComponentsStatus{
			Dashboard:        c.Status.Components.Dashboard,
			Workbenches:      c.Status.Components.Workbenches,
			AIPipelines:      c.Status.Components.DataSciencePipelines,
			Kserve:           c.Status.Components.Kserve,
			Kueue:            c.Status.Components.Kueue,
			Ray:              c.Status.Components.Ray,
			TrustyAI:         c.Status.Components.TrustyAI,
			ModelRegistry:    c.Status.Components.ModelRegistry,
			TrainingOperator: c.Status.Components.TrainingOperator,
			Trainer: componentApi.DSCTrainerStatus{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
			},
			FeastOperator:      c.Status.Components.FeastOperator,
			LlamaStackOperator: c.Status.Components.LlamaStackOperator,
		},
		Release: c.Status.Release,
	}

	return nil
}

// ConvertFrom converts the Hub version (v2) to this DataScienceCluster (v1).
func (c *DataScienceCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*dscv2.DataScienceCluster)

	c.ObjectMeta = src.ObjectMeta

	c.Spec = DataScienceClusterSpec{
		Components: Components{
			Dashboard:            src.Spec.Components.Dashboard,
			Workbenches:          src.Spec.Components.Workbenches,
			DataSciencePipelines: src.Spec.Components.AIPipelines,
			Kserve:               src.Spec.Components.Kserve,
			Kueue: DSCKueueV1{
				KueueManagementSpecV1: KueueManagementSpecV1{
					ManagementState: src.Spec.Components.Kueue.ManagementState,
				},
				KueueCommonSpec:       src.Spec.Components.Kueue.KueueCommonSpec,
				KueueDefaultQueueSpec: src.Spec.Components.Kueue.KueueDefaultQueueSpec,
			},
			Ray:                src.Spec.Components.Ray,
			TrustyAI:           src.Spec.Components.TrustyAI,
			ModelRegistry:      src.Spec.Components.ModelRegistry,
			TrainingOperator:   src.Spec.Components.TrainingOperator,
			FeastOperator:      src.Spec.Components.FeastOperator,
			LlamaStackOperator: src.Spec.Components.LlamaStackOperator,
		},
	}

	// Convert status with field renaming: AIPipelines -> DataSciencePipelines
	// and condition type renaming: AIPipelinesReady -> DataSciencePipelinesReady
	// and installed component name renaming: aipipelines -> data-science-pipelines-operator
	c.Status = DataScienceClusterStatus{
		Status: common.Status{
			Phase:              src.Status.Phase,
			ObservedGeneration: src.Status.ObservedGeneration,
			Conditions:         convertConditions(src.Status.Conditions, false),
		},
		RelatedObjects:      src.Status.RelatedObjects,
		ErrorMessage:        src.Status.ErrorMessage,
		InstalledComponents: constructInstalledComponentsFromV2Status(src.Status),
		Components: ComponentsStatus{
			Dashboard:            src.Status.Components.Dashboard,
			Workbenches:          src.Status.Components.Workbenches,
			DataSciencePipelines: src.Status.Components.AIPipelines,
			Kserve:               src.Status.Components.Kserve,
			Kueue:                src.Status.Components.Kueue,
			Ray:                  src.Status.Components.Ray,
			TrustyAI:             src.Status.Components.TrustyAI,
			ModelRegistry:        src.Status.Components.ModelRegistry,
			TrainingOperator:     src.Status.Components.TrainingOperator,
			FeastOperator:        src.Status.Components.FeastOperator,
			LlamaStackOperator:   src.Status.Components.LlamaStackOperator,
		},
		Release: src.Status.Release,
	}

	return nil
}
