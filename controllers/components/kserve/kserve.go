package kserve

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	componentName                   = componentsv1.KserveComponentName
	odhModelControllerComponentName = componentsv1.ModelControllerComponentName

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

func (s *componentHandler) Init(platform cluster.Platform) error {
	return nil
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

// for DSC to get compoment Kserve's CR.
func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	kserveAnnotations := make(map[string]string)
	kserveAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))

	return client.Object(&componentsv1.Kserve{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.KserveKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.KserveInstanceName,
			Annotations: kserveAnnotations,
		},
		Spec: componentsv1.KserveSpec{
			KserveCommonSpec: dsc.Spec.Components.Kserve.KserveCommonSpec,
		},
	})
}
