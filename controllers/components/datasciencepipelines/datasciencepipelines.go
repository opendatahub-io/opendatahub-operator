package datasciencepipelines

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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.DataSciencePipelinesComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.DataSciencePipelines.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) Init(_ common.Platform) error {
	release := cluster.GetRelease()

	extraParams := map[string]string{
		platformVersionParamsKey: release.Version.String(),
	}

	if err := deploy.ApplyParams(paramsPath, imageParamMap, extraParams); err != nil {
		return fmt.Errorf("failed to apply params on path %s: %w", paramsPath, err)
	}

	return nil
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	obj := componentApi.DataSciencePipelines{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.DataSciencePipelinesKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DataSciencePipelinesInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(s.GetManagementState(dsc)),
			},
		},
		Spec: componentApi.DataSciencePipelinesSpec{
			DataSciencePipelinesCommonSpec: dsc.Spec.Components.DataSciencePipelines.DataSciencePipelinesCommonSpec,
		},
	}

	// since the nested structures are not pointers, we must make sure
	// any field respect the validation rules.
	if obj.Spec.PreloadedPipelines.InstructLab.State == "" {
		obj.Spec.PreloadedPipelines.InstructLab.State = operatorv1.Removed
	}

	return &obj
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, cli *odhCli.Client, dsc *dscv1.DataScienceCluster, obj client.Object) error {
	c, ok := obj.(*componentApi.DataSciencePipelines)
	if !ok {
		return errors.New("failed to convert to DataSciencePipelines")
	}

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.DataSciencePipelines.ManagementSpec.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.DataSciencePipelines.DataSciencePipelinesCommonStatus = nil

	nc := conditionsv1.Condition{
		Type:    ReadyConditionType,
		Status:  corev1.ConditionFalse,
		Reason:  "Unknown",
		Message: "Not Available",
	}

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.DataSciencePipelines.DataSciencePipelinesCommonStatus = c.Status.DataSciencePipelinesCommonStatus.DeepCopy()

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
