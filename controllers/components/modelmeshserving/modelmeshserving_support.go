package modelmeshserving

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.ModelMeshServingComponentName
)

var (
	imageParamMap = map[string]string{
		"odh-mm-rest-proxy":             "RELATED_IMAGE_ODH_MM_REST_PROXY_IMAGE",
		"odh-modelmesh-runtime-adapter": "RELATED_IMAGE_ODH_MODELMESH_RUNTIME_ADAPTER_IMAGE",
		"odh-modelmesh":                 "RELATED_IMAGE_ODH_MODELMESH_IMAGE",
		"odh-modelmesh-controller":      "RELATED_IMAGE_ODH_MODELMESH_CONTROLLER_IMAGE",
	}
)

func manifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "overlays/odh",
	}
}
