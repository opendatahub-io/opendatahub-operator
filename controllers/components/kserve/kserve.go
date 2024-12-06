package kserve

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	componentName                   = componentApi.KserveComponentName
	odhModelControllerComponentName = componentApi.ModelControllerComponentName

	serviceMeshOperator = "servicemeshoperator"
	serverlessOperator  = "serverless-operator"

	kserveConfigMapName = "inferenceservice-config"

	kserveManifestSourcePath             = "overlays/odh"
	odhModelControllerManifestSourcePath = "base"
	finalizerName                        = "kserve.components.platform.opendatahub.io/finalizer"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

// Init for set images.
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
	var finalizerlist []string
	if s.GetManagementState(dsc) == operatorv1.Managed {
		finalizerlist = append(finalizerlist, finalizerName)
	}

	kserveAnnotations := make(map[string]string)
	kserveAnnotations[annotations.ManagementStateAnnotation] = string(s.GetManagementState(dsc))

	return client.Object(&componentApi.Kserve{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.KserveKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentApi.KserveInstanceName,
			Annotations: kserveAnnotations,
			Finalizers:  finalizerlist,
		},
		Spec: componentApi.KserveSpec{
			KserveCommonSpec: dsc.Spec.Components.Kserve.KserveCommonSpec,
		},
	})
}
