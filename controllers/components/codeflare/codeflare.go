package codeflare

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
	ComponentName = componentApi.CodeFlareComponentName
)

var (
	DefaultPath = odhdeploy.DefaultManifestPath + "/" + ComponentName + "/default" // same path for both odh and rhoai
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}
func (s *componentHandler) GetName() string {
	return componentApi.CodeFlareComponentName
}
func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.CodeFlare.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	codeflareAnnotations := make(map[string]string)
	codeflareAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))

	return client.Object(&componentApi.CodeFlare{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.CodeFlareKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentApi.CodeFlareInstanceName,
			Annotations: codeflareAnnotations,
		},
		Spec: componentApi.CodeFlareSpec{
			CodeFlareCommonSpec: dsc.Spec.Components.CodeFlare.CodeFlareCommonSpec,
		},
	})
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"codeflare-operator-controller-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE",
	}

	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}

func (s *componentHandler) Cleanup() error {
	return nil
}
