package modelmeshserving

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	ComponentName = componentsv1.ModelMeshServingComponentName
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	componentsregistry.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentsv1.ModelMeshServingComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.ModelMeshServing.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	DefaultPath := odhdeploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odh"
	imageParamMap := map[string]string{
		"odh-mm-rest-proxy":             "RELATED_IMAGE_ODH_MM_REST_PROXY_IMAGE",
		"odh-modelmesh-runtime-adapter": "RELATED_IMAGE_ODH_MODELMESH_RUNTIME_ADAPTER_IMAGE",
		"odh-modelmesh":                 "RELATED_IMAGE_ODH_MODELMESH_IMAGE",
		"odh-modelmesh-controller":      "RELATED_IMAGE_ODH_MODELMESH_CONTROLLER_IMAGE",
	}
	// Update image parameters
	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}

// for DSC to get compoment ModelMeshServing's CR.
func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	mmAnnotations := make(map[string]string)
	mmAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))

	return client.Object(&componentsv1.ModelMeshServing{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.ModelMeshServingKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.ModelMeshServingInstanceName,
			Annotations: mmAnnotations,
		},
		Spec: componentsv1.ModelMeshServingSpec{
			ModelMeshServingCommonSpec: dsc.Spec.Components.ModelMeshServing.ModelMeshServingCommonSpec,
		},
	})
}
