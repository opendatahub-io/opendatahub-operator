package codeflare

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
	ComponentName = componentsv1.CodeFlareComponentName
)

var (
	DefaultPath = odhdeploy.DefaultManifestPath + "/" + ComponentName + "/rhoai" // same path for both odh and rhoai
)

func GetComponentCR(dsc *dscv1.DataScienceCluster) *componentsv1.CodeFlare {
	codeflareAnnotations := make(map[string]string)
	switch dsc.Spec.Components.CodeFlare.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		codeflareAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.CodeFlare.ManagementState)
	default:
		codeflareAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.CodeFlare{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.CodeFlareKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.CodeFlareInstanceName,
			Annotations: codeflareAnnotations,
		},
		Spec: componentsv1.CodeFlareSpec{
			CodeFlareCommonSpec: dsc.Spec.Components.CodeFlare.CodeFlareCommonSpec,
		},
	}
}

func Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"codeflare-operator-controller-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE",
	}

	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}
