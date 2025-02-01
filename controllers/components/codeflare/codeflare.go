package codeflare

import (
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return &componentApi.CodeFlare{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.CodeFlareKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.CodeFlareInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(s.GetManagementState(dsc)),
			},
		},
		Spec: componentApi.CodeFlareSpec{
			CodeFlareCommonSpec: dsc.Spec.Components.CodeFlare.CodeFlareCommonSpec,
		},
	}
}

func (s *componentHandler) Init(_ common.Platform) error {
	if err := odhdeploy.ApplyParams(paramsPath, imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", paramsPath, err)
	}

	return nil
}

func (s *componentHandler) UpdateDSCStatus(dsc *dscv1.DataScienceCluster, obj client.Object) error {
	c, ok := obj.(*componentApi.CodeFlare)
	if !ok {
		return errors.New("failed to convert to CodeFlare")
	}

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.CodeFlare.ManagementSpec.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.CodeFlare.CodeFlareCommonStatus = nil

	nc := common.Condition{
		Type:    ReadyConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  status.UnknownReason,
		Message: "Not Available",
	}

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.CodeFlare.CodeFlareCommonStatus = c.Status.CodeFlareCommonStatus.DeepCopy()

		if rc := conditions.FindStatusCondition(c.Status.Conditions, status.ConditionTypeReady); rc != nil {
			nc.Status = rc.Status
			nc.Reason = rc.Reason
			nc.Message = rc.Message
		}

	case operatorv1.Removed:
		nc.Status = metav1.ConditionFalse
		nc.Reason = string(operatorv1.Removed)
		nc.Message = "Component ManagementState is set to " + string(operatorv1.Removed)

	default:
		return fmt.Errorf("unknown state %s ", s.GetManagementState(dsc))
	}

	conditions.SetStatusCondition(&dsc.Status.Conditions, nc)

	return nil
}
