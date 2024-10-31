package modelregistry

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

func Init(platform cluster.Platform) error {
	mi := baseManifestInfo(platform, BaseManifestsSourcePath)

	params := make(map[string]string)
	for k, v := range imagesMap {
		params[k] = v
	}
	for k, v := range extraParamsMap {
		params[k] = v
	}

	if err := odhdeploy.ApplyParams(mi.String(), params); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", mi, err)
	}

	return nil
}

func GetComponentCR(dsc *dscv1.DataScienceCluster) *componentsv1.ModelRegistry {
	componentAnnotations := make(map[string]string)

	switch dsc.Spec.Components.ModelRegistry.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		componentAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.ModelRegistry.ManagementState)
	default:
		// Force and Unmanaged case for unknown values, we do not support these yet
		componentAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.ModelRegistry{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.ModelRegistryKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.ModelRegistryInstanceName,
			Annotations: componentAnnotations,
		},
		Spec: componentsv1.ModelRegistrySpec{
			ModelRegistryCommonSpec: dsc.Spec.Components.ModelRegistry.ModelRegistryCommonSpec,
		},
	}
}
