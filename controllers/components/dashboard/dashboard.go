package dashboard

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

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentsv1.DashboardComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	return dsc.Spec.Components.Dashboard.ManagementState
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	mi := defaultManifestInfo(platform)

	if err := odhdeploy.ApplyParams(mi.String(), imagesMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", manifestPaths[platform], err)
	}

	return nil
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object { //nolint:ireturn
	dashboardAnnotations := make(map[string]string)

	switch dsc.Spec.Components.Dashboard.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		dashboardAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.Dashboard.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		dashboardAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return client.Object(&componentsv1.Dashboard{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.DashboardKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.DashboardInstanceName,
			Annotations: dashboardAnnotations,
		},
		Spec: componentsv1.DashboardSpec{
			DashboardCommonSpec: dsc.Spec.Components.Dashboard.DashboardCommonSpec,
		},
	})
}
