package ray

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	ComponentName = componentsv1.RayComponentName
)

var (
	DefaultPath = odhdeploy.DefaultManifestPath + "/" + ComponentName + "/openshift"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentsv1.RayComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) string {
	if s == nil || dsc.Spec.Components.Ray.ManagementState == operatorv1.Removed {
		return string(operatorv1.Removed)
	}
	switch dsc.Spec.Components.Ray.ManagementState {
	case operatorv1.Managed:
		return string(dsc.Spec.Components.Ray.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		return "Unknown"
	}
}
func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	rayAnnotations := make(map[string]string)
	rayAnnotations[annotations.ManagementStateAnnotation] = s.GetManagementState(dsc)

	return client.Object(&componentsv1.Ray{
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
	})
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
	}

	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}
