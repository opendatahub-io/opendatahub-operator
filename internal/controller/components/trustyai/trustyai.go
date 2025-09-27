package trustyai

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
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	// Evaluation permission constants.
	EvalPermissionAllow = "allow"
	EvalPermissionDeny  = "deny"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentApi.TrustyAIComponentName
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	// Create a proper deep copy to avoid modifying the original DSC
	spec := componentApi.TrustyAICommonSpec{
		DevFlagsSpec: dsc.Spec.Components.TrustyAI.DevFlagsSpec,
	}

	// Copy eval section exactly as it exists in the DSC
	spec.Eval = dsc.Spec.Components.TrustyAI.Eval

	// Ensure defaults are applied when strings are empty
	if spec.Eval.LMEval.PermitCodeExecution == "" {
		spec.Eval.LMEval.PermitCodeExecution = EvalPermissionDeny
	}
	if spec.Eval.LMEval.PermitOnline == "" {
		spec.Eval.LMEval.PermitOnline = EvalPermissionDeny
	}

	return &componentApi.TrustyAI{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.TrustyAIKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.TrustyAIInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(dsc.Spec.Components.TrustyAI.ManagementState),
			},
		},
		Spec: componentApi.TrustyAISpec{
			TrustyAICommonSpec: spec,
		},
	}
}

func (s *componentHandler) Init(platform common.Platform) error {
	mp := manifestsPath(platform)

	if err := odhdeploy.ApplyParams(mp.String(), "params.env", imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", mp, err)
	}

	return nil
}

func (s *componentHandler) IsEnabled(dsc *dscv1.DataScienceCluster) bool {
	return dsc.Spec.Components.TrustyAI.ManagementState == operatorv1.Managed
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.TrustyAI{}
	c.Name = componentApi.TrustyAIInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv1.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.TrustyAI.ManagementState)

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.TrustyAI.ManagementState = ms
	dsc.Status.Components.TrustyAI.TrustyAICommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	if s.IsEnabled(dsc) {
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.TrustyAI.TrustyAICommonStatus = c.Status.TrustyAICommonStatus.DeepCopy()

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
