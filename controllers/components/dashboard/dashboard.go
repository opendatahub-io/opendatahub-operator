package dashboard

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

func Init(platform cluster.Platform) error {
	mi := defaultManifestInfo(platform)

	if err := odhdeploy.ApplyParams(mi.String(), imagesMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", manifestPaths[platform], err)
	}

	return nil
}

func GetComponentCR(dsc *dscv1.DataScienceCluster) *componentsv1.Dashboard {
	dashboardAnnotations := make(map[string]string)

	switch dsc.Spec.Components.Dashboard.ManagementState {
	case operatorv1.Managed, operatorv1.Removed:
		dashboardAnnotations[annotations.ManagementStateAnnotation] = string(dsc.Spec.Components.Dashboard.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		dashboardAnnotations[annotations.ManagementStateAnnotation] = "Unknown"
	}

	return &componentsv1.Dashboard{
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
	}
}
