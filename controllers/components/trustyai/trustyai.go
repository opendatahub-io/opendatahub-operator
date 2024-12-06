package trustyai

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	ComponentName     = componentApi.TrustyAIComponentName
	ComponentPathName = "trustyai-service-operator"
)

var (
	SourcePath = map[cluster.Platform]string{
		cluster.SelfManagedRhoai: "/overlays/rhoai",
		cluster.ManagedRhoai:     "/overlays/rhoai",
		cluster.OpenDataHub:      "/overlays/odh",
		cluster.Unknown:          "/overlays/odh",
	}
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.TrustyAIComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.TrustyAI.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	trustyaiAnnotations := make(map[string]string)
	trustyaiAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))
	return client.Object(&componentApi.TrustyAI{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.TrustyAIKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentApi.TrustyAIInstanceName,
			Annotations: trustyaiAnnotations,
		},
		Spec: componentApi.TrustyAISpec{
			TrustyAICommonSpec: dsc.Spec.Components.TrustyAI.TrustyAICommonSpec,
		},
	})
}

func (s *componentHandler) Init(platform cluster.Platform) error {
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
