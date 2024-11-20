package kueue

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
	ComponentName = componentsv1.KueueComponentName
)

var (
	DefaultPath = odhdeploy.DefaultManifestPath + "/" + ComponentName + "/rhoai" // same path for both odh and rhoai
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentsv1.KueueComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	return dsc.Spec.Components.Kueue.ManagementState
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object { //nolint:ireturn
	kueueAnnotations := make(map[string]string)
	switch dsc.Spec.Components.Kueue.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		kueueAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.Kueue.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		kueueAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return client.Object(&componentsv1.Kueue{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.KueueKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.KueueInstanceName,
			Annotations: kueueAnnotations,
		},
		Spec: componentsv1.KueueSpec{
			KueueCommonSpec: dsc.Spec.Components.Kueue.KueueCommonSpec,
		},
	})
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	imageParamMap := map[string]string{
		"odh-kueue-controller-image": "RELATED_IMAGE_ODH_KUEUE_CONTROLLER_IMAGE",
	}

	if err := odhdeploy.ApplyParams(DefaultPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", DefaultPath, err)
	}

	return nil
}
