/*
Copyright 2025.

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

package v2

import (
	"slices"

	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
)

const (
	aiHubReadyCondition         = "AIHubReady"
	modelRegistryReadyCondition = "ModelRegistryReady"
)

func withConditionTypeMapping(status common.Status, mappings map[string]string) common.Status {
	if status.Conditions == nil {
		return status
	}

	mappedTypes := make(map[string]bool, len(mappings))
	for _, condition := range status.Conditions {
		if target, ok := mappings[condition.Type]; ok {
			mappedTypes[target] = true
		}
	}

	conditions := make([]common.Condition, 0, len(status.Conditions))
	conditionIndexes := make(map[string]int, len(status.Conditions))

	for _, condition := range status.Conditions {
		mapped := false
		if target, ok := mappings[condition.Type]; ok {
			condition.Type = target
			mapped = true
		}

		if !mapped && mappedTypes[condition.Type] {
			continue
		}

		if index, ok := conditionIndexes[condition.Type]; ok {
			if mapped {
				conditions[index] = condition
			}
			continue
		}

		conditionIndexes[condition.Type] = len(conditions)
		conditions = append(conditions, condition)
	}

	status.Conditions = conditions
	return status
}

func modelRegistryToAIHub(src componentApi.DSCModelRegistry) componentApi.DSCAIHub {
	return componentApi.DSCAIHub{
		ManagementSpec: src.ManagementSpec,
		AIHubCommonSpec: componentApi.AIHubCommonSpec{
			ApplicationNamespace: src.RegistriesNamespace,
		},
	}
}

func aiHubToModelRegistry(src componentApi.DSCAIHub) componentApi.DSCModelRegistry {
	return componentApi.DSCModelRegistry{
		ManagementSpec: src.ManagementSpec,
		ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
			RegistriesNamespace: src.ApplicationNamespace,
		},
	}
}

func modelRegistryStatusToAIHub(src componentApi.DSCModelRegistryStatus) componentApi.DSCAIHubStatus {
	dst := componentApi.DSCAIHubStatus{ManagementSpec: src.ManagementSpec}
	if src.ModelRegistryCommonStatus == nil {
		return dst
	}

	dst.AIHubCommonStatus = &componentApi.AIHubCommonStatus{
		ApplicationNamespace: src.RegistriesNamespace,
		ComponentReleaseStatus: common.ComponentReleaseStatus{
			Releases: slices.Clone(src.Releases),
		},
	}
	return dst
}

func aiHubStatusToModelRegistry(src componentApi.DSCAIHubStatus) componentApi.DSCModelRegistryStatus {
	dst := componentApi.DSCModelRegistryStatus{ManagementSpec: src.ManagementSpec}
	if src.AIHubCommonStatus == nil {
		return dst
	}

	dst.ModelRegistryCommonStatus = &componentApi.ModelRegistryCommonStatus{
		RegistriesNamespace: src.ApplicationNamespace,
		ComponentReleaseStatus: common.ComponentReleaseStatus{
			Releases: slices.Clone(src.Releases),
		},
	}
	return dst
}

func dashboardV2ToV3(src componentApi.DSCDashboardV2) componentApi.DSCDashboard {
	return componentApi.DSCDashboard{
		DashboardCommonSpec: componentApi.DashboardCommonSpec{
			Standard: componentApi.DashboardStandardSpec{
				ManagementSpec: src.ManagementSpec,
			},
			MaaSPortal: componentApi.DashboardMaaSPortalSpec{
				ManagementSpec: src.MaaSConsumerPortal.ManagementSpec,
			},
		},
	}
}

func dashboardV3ToV2(src componentApi.DSCDashboard) componentApi.DSCDashboardV2 {
	return componentApi.DSCDashboardV2{
		ManagementSpec: src.Standard.ManagementSpec,
		DashboardCommonSpecV2: componentApi.DashboardCommonSpecV2{
			MaaSConsumerPortal: componentApi.MaaSConsumerPortalSpec{
				ManagementSpec: src.MaaSPortal.ManagementSpec,
			},
		},
	}
}

// ConvertTo copies the DSC into v3, moving legacy MaaS selection into the canonical stanza.
func (c *DataScienceCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dscv3.DataScienceCluster)
	src := c.DeepCopy()

	dst.ObjectMeta = src.ObjectMeta
	dst.Spec = dscv3.DataScienceClusterSpec{
		Components: dscv3.Components{
			Dashboard:   dashboardV2ToV3(src.Spec.Components.Dashboard),
			Workbenches: src.Spec.Components.Workbenches,
			AIPipelines: src.Spec.Components.AIPipelines,
			Kserve: dscv3.DSCKserve{
				ManagementSpec: src.Spec.Components.Kserve.ManagementSpec,
				KserveCommonSpec: dscv3.KserveCommonSpec{
					RawDeploymentServiceConfig:   src.Spec.Components.Kserve.RawDeploymentServiceConfig,
					OAuthProxy:                   src.Spec.Components.Kserve.OAuthProxy,
					NIM:                          src.Spec.Components.Kserve.NIM,
					WVA:                          src.Spec.Components.Kserve.WVA,
					EnableLLMInferenceServiceTLS: src.Spec.Components.Kserve.EnableLLMInferenceServiceTLS,
					EnableLLMInferenceServiceConsoleDashboards: src.Spec.Components.Kserve.EnableLLMInferenceServiceConsoleDashboards,
					ModelCache: src.Spec.Components.Kserve.ModelCache,
				},
			},
			Kueue:    src.Spec.Components.Kueue,
			Ray:      src.Spec.Components.Ray,
			TrustyAI: src.Spec.Components.TrustyAI,
			AIHub:    modelRegistryToAIHub(src.Spec.Components.ModelRegistry),
			Data: componentApi.DSCData{
				FeatureStore: componentApi.DSCFeatureStore{
					ManagementSpec: src.Spec.Components.FeastOperator.ManagementSpec,
				},
				DataRegistry: src.Spec.Components.FeastOperator.DataRegistry,
			},
			OGX:                  src.Spec.Components.OGX,
			MLflowOperator:       src.Spec.Components.MLflowOperator,
			Trainer:              src.Spec.Components.Trainer,
			SparkOperator:        src.Spec.Components.SparkOperator,
			AIGateway:            src.Spec.Components.AIGateway,
			MCPLifecycleOperator: src.Spec.Components.MCPLifecycleOperator,
		},
	}
	dst.Status = dscv3.DataScienceClusterStatus{
		Status: withConditionTypeMapping(src.Status.Status, map[string]string{
			modelRegistryReadyCondition: aiHubReadyCondition,
		}),
		RelatedObjects: src.Status.RelatedObjects,
		ErrorMessage:   src.Status.ErrorMessage,
		Components: dscv3.ComponentsStatus{
			Dashboard:            src.Status.Components.Dashboard,
			MaaSConsumerPortal:   src.Status.Components.MaaSConsumerPortal,
			Workbenches:          src.Status.Components.Workbenches,
			WorkbenchesV2:        src.Status.Components.WorkbenchesV2,
			AIPipelines:          src.Status.Components.AIPipelines,
			Kserve:               src.Status.Components.Kserve,
			Kueue:                src.Status.Components.Kueue,
			Ray:                  src.Status.Components.Ray,
			TrustyAI:             src.Status.Components.TrustyAI,
			AIHub:                modelRegistryStatusToAIHub(src.Status.Components.ModelRegistry),
			FeastOperator:        src.Status.Components.FeastOperator,
			OGX:                  src.Status.Components.OGX,
			MLflowOperator:       src.Status.Components.MLflowOperator,
			Trainer:              src.Status.Components.Trainer,
			SparkOperator:        src.Status.Components.SparkOperator,
			AIGateway:            src.Status.Components.AIGateway,
			ModelsAsAService:     src.Status.Components.ModelsAsAService,
			BatchGateway:         src.Status.Components.BatchGateway,
			MCPLifecycleOperator: src.Status.Components.MCPLifecycleOperator,
		},
		Release: src.Status.Release,
	}

	canonical := src.Spec.Components.AIGateway.ModelsAsAService
	legacy := src.Spec.Components.Kserve.ModelsAsService.ManagementState //nolint:staticcheck // v2 compatibility input.

	switch {
	case canonical.ManagementState == operatorv1.Managed,
		src.Spec.Components.Kserve.ManagementState == operatorv1.Managed && legacy == operatorv1.Managed:
		dst.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	default:
		dst.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	}

	switch {
	case src.Spec.Components.AIGateway.ManagementState != "":
		dst.Spec.Components.AIGateway.ManagementState = src.Spec.Components.AIGateway.ManagementState
	case src.Spec.Components.Kserve.ManagementState == operatorv1.Managed && legacy == operatorv1.Managed:
		dst.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
	}

	dst.PreserveMaaSV2State(canonical.ManagementState, legacy)

	return nil
}

// ConvertFrom restores the v2 MaaS source view while keeping the migrated AI Gateway parent.
func (c *DataScienceCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*dscv3.DataScienceCluster).DeepCopy()
	dst := c

	dst.ObjectMeta = src.ObjectMeta
	dst.Spec = DataScienceClusterSpec{
		Components: Components{
			Dashboard:   dashboardV3ToV2(src.Spec.Components.Dashboard),
			Workbenches: src.Spec.Components.Workbenches,
			AIPipelines: src.Spec.Components.AIPipelines,
			Kserve: componentApi.DSCKserve{
				ManagementSpec: src.Spec.Components.Kserve.ManagementSpec,
				KserveCommonSpec: componentApi.KserveCommonSpec{
					RawDeploymentServiceConfig:   src.Spec.Components.Kserve.RawDeploymentServiceConfig,
					OAuthProxy:                   src.Spec.Components.Kserve.OAuthProxy,
					NIM:                          src.Spec.Components.Kserve.NIM,
					WVA:                          src.Spec.Components.Kserve.WVA,
					EnableLLMInferenceServiceTLS: src.Spec.Components.Kserve.EnableLLMInferenceServiceTLS,
					EnableLLMInferenceServiceConsoleDashboards: src.Spec.Components.Kserve.EnableLLMInferenceServiceConsoleDashboards,
					ModelCache:      src.Spec.Components.Kserve.ModelCache,
					ModelsAsService: componentApi.DSCModelsAsServiceSpec{ManagementState: operatorv1.Removed},
				},
			},
			Kueue:         src.Spec.Components.Kueue,
			Ray:           src.Spec.Components.Ray,
			TrustyAI:      src.Spec.Components.TrustyAI,
			ModelRegistry: aiHubToModelRegistry(src.Spec.Components.AIHub),
			FeastOperator: componentApi.DSCFeastOperator{
				ManagementSpec: src.Spec.Components.Data.FeatureStore.ManagementSpec,
				DataRegistry:   src.Spec.Components.Data.DataRegistry,
			},
			OGX:                  src.Spec.Components.OGX,
			MLflowOperator:       src.Spec.Components.MLflowOperator,
			Trainer:              src.Spec.Components.Trainer,
			SparkOperator:        src.Spec.Components.SparkOperator,
			AIGateway:            src.Spec.Components.AIGateway,
			MCPLifecycleOperator: src.Spec.Components.MCPLifecycleOperator,
		},
	}
	dst.Status = DataScienceClusterStatus{
		Status: withConditionTypeMapping(src.Status.Status, map[string]string{
			aiHubReadyCondition: modelRegistryReadyCondition,
		}),
		RelatedObjects: src.Status.RelatedObjects,
		ErrorMessage:   src.Status.ErrorMessage,
		Components: ComponentsStatus{
			Dashboard:            src.Status.Components.Dashboard,
			MaaSConsumerPortal:   src.Status.Components.MaaSConsumerPortal,
			Workbenches:          src.Status.Components.Workbenches,
			WorkbenchesV2:        src.Status.Components.WorkbenchesV2,
			AIPipelines:          src.Status.Components.AIPipelines,
			Kserve:               src.Status.Components.Kserve,
			Kueue:                src.Status.Components.Kueue,
			Ray:                  src.Status.Components.Ray,
			TrustyAI:             src.Status.Components.TrustyAI,
			ModelRegistry:        aiHubStatusToModelRegistry(src.Status.Components.AIHub),
			FeastOperator:        src.Status.Components.FeastOperator,
			OGX:                  src.Status.Components.OGX,
			MLflowOperator:       src.Status.Components.MLflowOperator,
			Trainer:              src.Status.Components.Trainer,
			SparkOperator:        src.Status.Components.SparkOperator,
			AIGateway:            src.Status.Components.AIGateway,
			ModelsAsAService:     src.Status.Components.ModelsAsAService,
			BatchGateway:         src.Status.Components.BatchGateway,
			MCPLifecycleOperator: src.Status.Components.MCPLifecycleOperator,
		},
		Release: src.Status.Release,
	}
	dst.Spec.Components.TrainingOperator.ManagementState = operatorv1.Removed
	dst.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Removed
	dst.Status.Components.TrainingOperator.ManagementState = operatorv1.Removed
	dst.Status.Components.LlamaStackOperator.ManagementState = operatorv1.Removed

	legacyManaged, err := src.ReadMaaSV2State()
	if err != nil {
		return err
	}

	if legacyManaged {
		dst.Spec.Components.Kserve.ModelsAsService.ManagementState = operatorv1.Managed //nolint:staticcheck // Preserve legacy provenance.
		dst.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	}

	delete(dst.Annotations, dscv3.MaaSV2StateAnnotation)
	if len(dst.Annotations) == 0 {
		dst.Annotations = nil
	}

	return nil
}
