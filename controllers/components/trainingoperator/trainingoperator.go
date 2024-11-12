package trainingoperator

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
	ComponentName = componentsv1.TrainingOperatorComponentName
)

var (
	DefaultPath = odhdeploy.DefaultManifestPath + "/" + ComponentName + "/rhoai"
)

// for DSC to get compoment TrainingOperator's CR.
func GetComponentCR(dsc *dscv1.DataScienceCluster) *componentsv1.TrainingOperator {
	trainingoperatorAnnotations := make(map[string]string)
	switch dsc.Spec.Components.TrainingOperator.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		trainingoperatorAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.TrainingOperator.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		trainingoperatorAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.TrainingOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.TrainingOperatorKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.TrainingOperatorInstanceName,
			Annotations: trainingoperatorAnnotations,
		},
		Spec: componentsv1.TrainingOperatorSpec{
			TrainingOperatorCommonSpec: dsc.Spec.Components.TrainingOperator.TrainingOperatorCommonSpec,
		},
	}
}

// Init for set images.
func Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"odh-training-operator-controller-image": "RELATED_IMAGE_ODH_TRAINING_OPERATOR_IMAGE",
	}

	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}
