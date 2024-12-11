package trainingoperator

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	ComponentName = componentApi.TrainingOperatorComponentName
	finalizerName = "trainingoperator.components.platform.opendatahub.io/finalizer"
)

var (
	DefaultPath = odhdeploy.DefaultManifestPath + "/" + ComponentName + "/rhoai"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.TrainingOperatorComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.TrainingOperator.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}
func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) k8sclient.Object {
	var finalizerlist []string
	if s.GetManagementState(dsc) == operatorv1.Managed {
		finalizerlist = append(finalizerlist, finalizerName)
	}

	trainingoperatorAnnotations := make(map[string]string)
	trainingoperatorAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))

	return k8sclient.Object(&componentApi.TrainingOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.TrainingOperatorKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentApi.TrainingOperatorInstanceName,
			Annotations: trainingoperatorAnnotations,
			Finalizers:  finalizerlist,
		},
		Spec: componentApi.TrainingOperatorSpec{
			TrainingOperatorCommonSpec: dsc.Spec.Components.TrainingOperator.TrainingOperatorCommonSpec,
		},
	})
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"odh-training-operator-controller-image": "RELATED_IMAGE_ODH_TRAINING_OPERATOR_IMAGE",
	}

	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}
