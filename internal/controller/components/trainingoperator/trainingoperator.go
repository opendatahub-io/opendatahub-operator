package trainingoperator

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

type componentHandler struct{}

func NewHandler() *componentHandler { return &componentHandler{} }

func (s *componentHandler) GetName() string {
	return componentApi.TrainingOperatorComponentName
}

func (s *componentHandler) NewCRObject(_ context.Context, _ client.Client, dsc *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return &componentApi.TrainingOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.TrainingOperatorKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.TrainingOperatorInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(dsc.Spec.Components.TrainingOperator.ManagementState),
			},
		},
		Spec: componentApi.TrainingOperatorSpec{
			TrainingOperatorCommonSpec: dsc.Spec.Components.TrainingOperator.TrainingOperatorCommonSpec,
		},
	}, nil
}

// Deprecated: Training Operator v1 is obsolete in RHOAI 3.6. Init is a no-op stub.
func (s *componentHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error {
	return nil
}

// Deprecated: Training Operator v1 is obsolete in RHOAI 3.6.
// Managed: keep CR alive so existing deployment is untouched (no GC teardown).
// Removed: return false, framework GC deletes CR, cascade GC tears down owned resources.
func (s *componentHandler) IsEnabled(dsc *dscv2.DataScienceCluster) bool {
	return dsc.Spec.Components.TrainingOperator.ManagementState == operatorv1.Managed
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.TrainingOperator{}
	c.Name = componentApi.TrainingOperatorInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.TrainingOperator.ManagementState)

	dsc.Status.Components.TrainingOperator.ManagementState = ms
	dsc.Status.Components.TrainingOperator.TrainingOperatorCommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	if !c.GetDeletionTimestamp().IsZero() {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.DeletingReason),
			conditions.WithMessage(status.DeletingMessage),
		)
		return metav1.ConditionFalse, nil
	}

	if s.IsEnabled(dsc) {
		log := logf.FromContext(ctx)
		log.Info("TrainingOperator v1 is obsolete in RHOAI 3.6, customer must set managementState to Removed to uninstall")

		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason("Obsolete"),
			conditions.WithMessage("Training Operator v1 is obsolete in RHOAI 3.6. Set managementState to Removed to uninstall, then use Trainer v2."),
		)

		cs = metav1.ConditionFalse
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
