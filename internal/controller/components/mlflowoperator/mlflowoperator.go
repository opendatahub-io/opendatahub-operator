package mlflowoperator

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
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
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
	return componentApi.MLflowOperatorComponentName
}

func (s *componentHandler) NewCRObject(dsc *dscv2.DataScienceCluster) common.PlatformObject {
	return &componentApi.MLflowOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.MLflowOperatorKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.MLflowOperatorInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(dsc.Spec.Components.MLflowOperator.ManagementState),
			},
		},
		Spec: componentApi.MLflowOperatorSpec{
			MLflowOperatorCommonSpec: dsc.Spec.Components.MLflowOperator.MLflowOperatorCommonSpec,
		},
	}
}

func (s *componentHandler) Init(platform common.Platform) error {
	if err := odhdeploy.ApplyParams(manifestPath(platform).String(), "params.env", imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", manifestPath(platform), err)
	}

	return nil
}

func (s *componentHandler) IsEnabled(dsc *dscv2.DataScienceCluster) bool {
	return dsc.Spec.Components.MLflowOperator.ManagementState == operatorv1.Managed
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.MLflowOperator{}
	c.Name = componentApi.MLflowOperatorInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.MLflowOperator.ManagementState)

	dsc.Status.Components.MLflowOperator.ManagementState = ms
	dsc.Status.Components.MLflowOperator.MLflowOperatorCommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	if s.IsEnabled(dsc) {
		dsc.Status.Components.MLflowOperator.MLflowOperatorCommonStatus = c.Status.MLflowOperatorCommonStatus.DeepCopy()

		if rc := conditions.FindStatusCondition(c.GetStatus(), status.ConditionTypeReady); rc != nil {
			rr.Conditions.MarkFrom(ReadyConditionType, *rc)
			cs = rc.Status
		} else {
			cs = metav1.ConditionFalse
		}
	} else {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(string(ms)),
			conditions.WithMessage("Component ManagementState is set to %s", string(ms)),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)
	}

	return cs, nil
}
