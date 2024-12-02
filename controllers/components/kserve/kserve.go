package kserve

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	componentName                   = componentsv1alpha1.KserveComponentName
	odhModelControllerComponentName = componentsv1alpha1.ModelControllerComponentName

	serviceMeshOperator = "servicemeshoperator"
	serverlessOperator  = "serverless-operator"

	kserveConfigMapName = "inferenceservice-config"

	kserveManifestSourcePath             = "overlays/odh"
	odhModelControllerManifestSourcePath = "base"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.Kserve.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

// Init for set images.
func (s *componentHandler) Init(platform cluster.Platform) error {
	omcManifestInfo := odhModelControllerManifestInfo(odhModelControllerManifestSourcePath)

	// dependentParamMap for odh-model-controller to use.
	var dependentParamMap = map[string]string{
		odhModelControllerComponentName: "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
	}

	// Update image parameters for odh-model-controller
	if err := deploy.ApplyParams(omcManifestInfo.String(), dependentParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", omcManifestInfo.String(), err)
	}

	return nil
}

// for DSC to get compoment Kserve's CR.
func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	kserveAnnotations := make(map[string]string)
	kserveAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))

	return client.Object(&componentsv1alpha1.Kserve{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1alpha1.KserveKind,
			APIVersion: componentsv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1alpha1.KserveInstanceName,
			Annotations: kserveAnnotations,
		},
		Spec: componentsv1alpha1.KserveSpec{
			KserveCommonSpec: dsc.Spec.Components.Kserve.KserveCommonSpec,
		},
	})
}
