package dashboard

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
	return componentApi.DashboardComponentName
}

func (s *componentHandler) Init(platform common.Platform) error {
	mi := defaultManifestInfo(platform)

	if err := odhdeploy.ApplyParams(mi.String(), "params.env", imagesMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", mi, err)
	}

	extra := bffManifestsPath()
	if err := odhdeploy.ApplyParams(extra.String(), "params.env", imagesMap); err != nil {
		return fmt.Errorf("failed to update modular-architecture images on path %s: %w", extra, err)
	}

	return nil
}

func (s *componentHandler) NewCRObject(dsc *dscv2.DataScienceCluster) common.PlatformObject {
	return &componentApi.Dashboard{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.DashboardKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DashboardInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(dsc.Spec.Components.Dashboard.ManagementState),
			},
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: dsc.Spec.Components.Dashboard.DashboardCommonSpec,
		},
	}
}

func (s *componentHandler) IsEnabled(dsc *dscv2.DataScienceCluster) bool {
	return dsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed
}

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.Dashboard{}
	c.Name = componentApi.DashboardInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.Dashboard.ManagementState)

	dsc.Status.InstalledComponents[LegacyComponentNameUpstream] = false
	dsc.Status.Components.Dashboard.ManagementState = ms
	dsc.Status.Components.Dashboard.DashboardCommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	if s.IsEnabled(dsc) {
		dsc.Status.InstalledComponents[LegacyComponentNameUpstream] = true
		dsc.Status.Components.Dashboard.DashboardCommonStatus = c.Status.DashboardCommonStatus.DeepCopy()

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
