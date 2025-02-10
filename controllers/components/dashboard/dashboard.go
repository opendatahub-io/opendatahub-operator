package dashboard

import (
	"context"
	"errors"
	"fmt"
	odhCli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"

	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.DashboardComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) Init(platform common.Platform) error {
	mi := defaultManifestInfo(platform)

	if err := odhdeploy.ApplyParams(mi.String(), imagesMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", mi, err)
	}

	return nil
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return &componentApi.Dashboard{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.DashboardKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DashboardInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(s.GetManagementState(dsc)),
			},
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: dsc.Spec.Components.Dashboard.DashboardCommonSpec,
		},
	}
}

func (s *componentHandler) UpdateDSCStatus(_ context.Context, _ *odhCli.Client, dsc *dscv1.DataScienceCluster, obj client.Object) error {
	c, ok := obj.(*componentApi.Dashboard)
	if !ok {
		return errors.New("failed to convert to Dashboard")
	}

	dsc.Status.InstalledComponents[LegacyComponentNameUpstream] = false
	dsc.Status.Components.Dashboard.ManagementSpec.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.Dashboard.DashboardCommonStatus = nil

	nc := conditionsv1.Condition{
		Type:    ReadyConditionType,
		Status:  corev1.ConditionFalse,
		Reason:  "Unknown",
		Message: "Not Available",
	}

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[LegacyComponentNameUpstream] = true
		dsc.Status.Components.Dashboard.DashboardCommonStatus = c.Status.DashboardCommonStatus.DeepCopy()

		if rc := meta.FindStatusCondition(c.Status.Conditions, status.ConditionTypeReady); rc != nil {
			nc.Status = corev1.ConditionStatus(rc.Status)
			nc.Reason = rc.Reason
			nc.Message = rc.Message
		}

	case operatorv1.Removed:
		nc.Status = corev1.ConditionFalse
		nc.Reason = string(operatorv1.Removed)
		nc.Message = "Component ManagementState is set to " + string(operatorv1.Removed)

	default:
		return fmt.Errorf("unknown state %s ", s.GetManagementState(dsc))
	}

	conditionsv1.SetStatusCondition(&dsc.Status.Conditions, nc)

	return nil
}
