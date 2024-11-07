package trustyai

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

const (
	ComponentName     = componentsv1.TrustyAIComponentName
	ComponentPathName = "trustyai-service-operator"
)

var (
	SourcePath = map[cluster.Platform]string{
		cluster.SelfManagedRhods: "/overlays/rhoai",
		cluster.ManagedRhods:     "/overlays/rhoai",
		cluster.OpenDataHub:      "/overlays/odh",
		cluster.Unknown:          "/overlays/odh",
	}
)

// for DSC to get compoment TrustyAI's CR.
func GetComponentCR(dsc *dscv1.DataScienceCluster) *componentsv1.TrustyAI {
	trustyaiAnnotations := make(map[string]string)
	switch dsc.Spec.Components.TrustyAI.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		trustyaiAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.TrustyAI.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		trustyaiAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.TrustyAI{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.TrustyAIKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.TrustyAIInstanceName,
			Annotations: trustyaiAnnotations,
		},
		Spec: componentsv1.TrustyAISpec{
			TrustyAICommonSpec: dsc.Spec.Components.TrustyAI.TrustyAICommonSpec,
		},
	}
}

// Init for set images.
func Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"trustyaiServiceImage":  "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE",
		"trustyaiOperatorImage": "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE",
	}
	DefaultPath := odhdeploy.DefaultManifestPath + "/" + ComponentPathName + SourcePath[platform]
	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}
