package feastoperator

import (
	"context"
	"errors"
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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
	return componentApi.FeastOperatorComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.FeastOperator.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return &componentApi.FeastOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.FeastOperatorKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.FeastOperatorInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(s.GetManagementState(dsc)),
			},
		},
		Spec: componentApi.FeastOperatorSpec{
			FeastOperatorCommonSpec: dsc.Spec.Components.FeastOperator.FeastOperatorCommonSpec,
		},
	}
}

func (s *componentHandler) Init(_ common.Platform) error {
	if err := odhdeploy.ApplyParams(manifestPath().String(), imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", manifestPath(), err)
	}

	return nil
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, cli *odhCli.Client, dsc *dscv1.DataScienceCluster, obj client.Object) error {
	c, ok := obj.(*componentApi.FeastOperator)
	if !ok {
		return errors.New("failed to convert to FeastOperator")
	}

	dsc.Status.InstalledComponents[ComponentName] = false
	dsc.Status.Components.FeastOperator.ManagementSpec.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.FeastOperator.FeastOperatorCommonStatus = nil

	nc := conditionsv1.Condition{
		Type:    ReadyConditionType,
		Status:  corev1.ConditionFalse,
		Reason:  "Unknown",
		Message: "Not Available",
	}

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[ComponentName] = true
		dsc.Status.Components.FeastOperator.FeastOperatorCommonStatus = c.Status.FeastOperatorCommonStatus.DeepCopy()

		// TODO: This block can be refactored when we have support for mixed-arch
		isPowerArch, err := cluster.HasPowerArchNode(ctx, cli)
		if err != nil {
			return fmt.Errorf("unable to determine architecture %v", err)
		}
		if isPowerArch {
			nc.Status = status.ReconcileCompleted
			nc.Reason = status.UnsupportedArchitectureReason
			nc.Message = status.UnsupportedArchitectureMessage
		}

		conditionsv1.SetStatusCondition(&dsc.Status.Conditions, nc)
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
