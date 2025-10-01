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
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var componentNameMapping = map[string]string{
	"DataSciencePipelines": "AIPipelines",
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

// ConvertTo converts this DataScienceCluster (v1) to the Hub version (v2).
func (c *DataScienceCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dscv2.DataScienceCluster)

	dst.ObjectMeta = c.ObjectMeta

	dst.Spec = dscv2.DataScienceClusterSpec{
		Components: dscv2.Components{
			Dashboard:            c.Spec.Components.Dashboard,
			Workbenches:          c.Spec.Components.Workbenches,
			AIPipelines: c.Spec.Components.DataSciencePipelines,
			Kserve:               c.Spec.Components.Kserve,
			Kueue:                c.Spec.Components.Kueue,
			Ray:                  c.Spec.Components.Ray,
			TrustyAI:             c.Spec.Components.TrustyAI,
			ModelRegistry:        c.Spec.Components.ModelRegistry,
			TrainingOperator:     c.Spec.Components.TrainingOperator,
			FeastOperator:        c.Spec.Components.FeastOperator,
			LlamaStackOperator:   c.Spec.Components.LlamaStackOperator,
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
		RelatedObjects:      c.Status.RelatedObjects,
		ErrorMessage:        c.Status.ErrorMessage,
		InstalledComponents: c.Status.InstalledComponents,
		Components: dscv2.ComponentsStatus{
			Dashboard:            c.Status.Components.Dashboard,
			Workbenches:          c.Status.Components.Workbenches,
			AIPipelines: c.Status.Components.DataSciencePipelines,
			Kserve:               c.Status.Components.Kserve,
			Kueue:                c.Status.Components.Kueue,
			Ray:                  c.Status.Components.Ray,
			TrustyAI:             c.Status.Components.TrustyAI,
			ModelRegistry:        c.Status.Components.ModelRegistry,
			TrainingOperator:     c.Status.Components.TrainingOperator,
			FeastOperator:        c.Status.Components.FeastOperator,
			LlamaStackOperator:   c.Status.Components.LlamaStackOperator,
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
			DataSciencePipelines: src.Spec.Components.DataSciencePipelines,
			Kserve:               src.Spec.Components.Kserve,
			Kueue:                src.Spec.Components.Kueue,
			Ray:                  src.Spec.Components.Ray,
			TrustyAI:             src.Spec.Components.TrustyAI,
			ModelRegistry:        src.Spec.Components.ModelRegistry,
			TrainingOperator:     src.Spec.Components.TrainingOperator,
			FeastOperator:        src.Spec.Components.FeastOperator,
			LlamaStackOperator:   src.Spec.Components.LlamaStackOperator,
		},
	}

	// Convert status with field renaming: AIPipelines -> DataSciencePipelines
	// and condition type renaming: AIPipelinesReady -> DataSciencePipelinesReady
	c.Status = DataScienceClusterStatus{
		Status: common.Status{
			Phase:              src.Status.Phase,
			ObservedGeneration: src.Status.ObservedGeneration,
			Conditions:         convertConditions(src.Status.Conditions, false),
		},
		RelatedObjects:      src.Status.RelatedObjects,
		ErrorMessage:        src.Status.ErrorMessage,
		InstalledComponents: src.Status.InstalledComponents,
		Components: ComponentsStatus{
			Dashboard:            src.Status.Components.Dashboard,
			Workbenches:          src.Status.Components.Workbenches,
			DataSciencePipelines: src.Status.Components.DataSciencePipelines,
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
