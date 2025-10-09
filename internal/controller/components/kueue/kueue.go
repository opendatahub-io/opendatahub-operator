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
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	kueueOperator          = "kueue-operator"
	kueueOperatorNamespace = "openshift-kueue-operator"
	kueueCRDname           = "kueues.kueue.openshift.io"
)

var (
	ErrKueueStateManagedNotSupported = odherrors.NewStopError(status.KueueStateManagedNotSupportedMessage)
	ErrKueueOperatorNotInstalled     = odherrors.NewStopError(status.KueueOperatorNotInstalledMessage)
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.KueueComponentName
}

func (s *componentHandler) NewCRObject(dsc *dscv2.DataScienceCluster) common.PlatformObject {
	return &componentApi.Kueue{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.KueueKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.KueueInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(dsc.Spec.Components.Kueue.ManagementState),
			},
		},
		Spec: componentApi.KueueSpec{
			KueueManagementSpec:   dsc.Spec.Components.Kueue.KueueManagementSpec,
			KueueCommonSpec:       dsc.Spec.Components.Kueue.KueueCommonSpec,
			KueueDefaultQueueSpec: dsc.Spec.Components.Kueue.KueueDefaultQueueSpec,
		},
	}
}

func (s *componentHandler) Init(platform common.Platform) error {
	if err := odhdeploy.ApplyParams(manifestsPath().String(), "params.env", imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", manifestsPath(), err)
	}

	return nil
}

func (s *componentHandler) IsEnabled(dsc *dscv2.DataScienceCluster) bool {
	switch dsc.Spec.Components.Kueue.ManagementState {
	case operatorv1.Managed:
		return true
	case operatorv1.Unmanaged:
		return true
	default:
		return false
	}
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.Kueue{}
	c.Name = componentApi.KueueInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.Kueue.ManagementState)

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.Kueue.ManagementState = ms
	dsc.Status.Components.Kueue.KueueCommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	if s.IsEnabled(dsc) {
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.Kueue.KueueCommonStatus = c.Status.KueueCommonStatus.DeepCopy()

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
