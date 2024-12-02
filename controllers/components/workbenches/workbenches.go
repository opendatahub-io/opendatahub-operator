package workbenches

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentsv1alpha1.WorkbenchesComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.Workbenches.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	workbenchesAnnotations := make(map[string]string)
	workbenchesAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))

	return client.Object(&componentsv1alpha1.Workbenches{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1alpha1.WorkbenchesKind,
			APIVersion: componentsv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1alpha1.WorkbenchesInstanceName,
			Annotations: workbenchesAnnotations,
		},
		Spec: componentsv1alpha1.WorkbenchesSpec{
			WorkbenchesCommonSpec: dsc.Spec.Components.Workbenches.WorkbenchesCommonSpec,
		},
	})
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	nbcManifestInfo := notebookControllerManifestInfo(notebookControllerManifestSourcePath)
	if err := odhdeploy.ApplyParams(nbcManifestInfo.String(), map[string]string{
		"odh-notebook-controller-image": "RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE",
	}); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", nbcManifestInfo.String(), err)
	}

	kfNbcManifestInfo := kfNotebookControllerManifestInfo(kfNotebookControllerManifestSourcePath)
	if err := odhdeploy.ApplyParams(kfNbcManifestInfo.String(), map[string]string{
		"odh-kf-notebook-controller-image": "RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE",
	}); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", kfNbcManifestInfo.String(), err)
	}

	return nil
}
