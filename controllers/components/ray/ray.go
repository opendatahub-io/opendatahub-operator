package ray

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

// for DSC to get compoment Ray's CR.
func GetComponentCR(dsc *dscv1.DataScienceCluster) *componentsv1.Ray {
	rayAnnotations := make(map[string]string)
	switch dsc.Spec.Components.Ray.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		rayAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.Ray.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		rayAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.Ray{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.RayKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.RayInstanceName,
			Annotations: rayAnnotations,
		},
		Spec: componentsv1.RaySpec{
			RayCommonSpec: dsc.Spec.Components.Ray.RayCommonSpec,
		},
	}
}

// Init for set images.
func Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
	}

	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}
