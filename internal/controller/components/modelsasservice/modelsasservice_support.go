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

package modelsasservice

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.ModelsAsServiceComponentName

	ReadyConditionType = componentApi.ModelsAsServiceKind + status.ReadySuffix

	// Default Gateway values as specified in the spec
	DefaultGatewayNamespace = "openshift-ingress"
	DefaultGatewayName      = "maas-default-gateway"

	// Manifest paths
	BaseManifestsSourcePath = "overlays/openshift"
)

var (
	// Image parameter mappings for manifest substitution
	imagesMap = map[string]string{
		"IMAGES_MAAS_API": "RELATED_IMAGE_ODH_MAAS_API_IMAGE",
	}

	// Additional parameters for manifest customization
	extraParamsMap = map[string]string{
		"DEFAULT_GATEWAY_NAMESPACE": DefaultGatewayNamespace,
		"DEFAULT_GATEWAY_NAME":      DefaultGatewayName,
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func baseManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       deploy.DefaultManifestPath,
		ContextDir: "maas",
		SourcePath: sourcePath,
	}
}
