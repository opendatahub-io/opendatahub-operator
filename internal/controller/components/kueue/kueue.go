package kueue

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.KueueComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.Kueue.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return &componentApi.Kueue{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.KueueKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.KueueInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(s.GetManagementState(dsc)),
			},
		},
		Spec: componentApi.KueueSpec{
			KueueCommonSpec: dsc.Spec.Components.Kueue.KueueCommonSpec,
		},
	}
}

func (s *componentHandler) Init(platform common.Platform) error {
	if err := odhdeploy.ApplyParams(manifestsPath().String(), imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", manifestsPath(), err)
	}

	return nil
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.Kueue{}
	c.Name = componentApi.KueueInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv1.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.Kueue.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.Kueue.KueueCommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.Kueue.KueueCommonStatus = c.Status.KueueCommonStatus.DeepCopy()

		if rc := conditions.FindStatusCondition(c.GetStatus(), status.ConditionTypeReady); rc != nil {
			rr.Conditions.MarkFrom(ReadyConditionType, *rc)
			cs = rc.Status
		} else {
			cs = metav1.ConditionFalse
		}

	case operatorv1.Removed:
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(string(operatorv1.Removed)),
			conditions.WithMessage("Component ManagementState is set to %s", operatorv1.Removed),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)

	default:
		return cs, fmt.Errorf("unknown state %s ", s.GetManagementState(dsc))
	}

	return cs, nil
}
