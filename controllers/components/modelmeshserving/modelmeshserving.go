package modelmeshserving

import (
	"errors"
	"fmt"

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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	componentsregistry.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.ModelMeshServingComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.ModelMeshServing.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) Init(_ common.Platform) error {
	// Update image parameters
	if err := odhdeploy.ApplyParams(manifestsPath().String(), imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", manifestsPath(), err)
	}

	return nil
}

// for DSC to get compoment ModelMeshServing's CR.
func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return &componentApi.ModelMeshServing{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.ModelMeshServingKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.ModelMeshServingInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(s.GetManagementState(dsc)),
			},
		},
		Spec: componentApi.ModelMeshServingSpec{
			ModelMeshServingCommonSpec: dsc.Spec.Components.ModelMeshServing.ModelMeshServingCommonSpec,
		},
	}
}

func (s *componentHandler) UpdateDSCStatus(dsc *dscv1.DataScienceCluster, obj client.Object) error {
	c, ok := obj.(*componentApi.ModelMeshServing)
	if !ok {
		return errors.New("failed to convert to ModelMeshServing")
	}

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.ModelMeshServing.ManagementSpec.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.ModelMeshServing.ModelMeshServingCommonStatus = nil

	nc := conditionsv1.Condition{
		Type:    ReadyConditionType,
		Status:  corev1.ConditionFalse,
		Reason:  "Unknown",
		Message: "Not Available",
	}

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.ModelMeshServing.ModelMeshServingCommonStatus = c.Status.ModelMeshServingCommonStatus.DeepCopy()

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
