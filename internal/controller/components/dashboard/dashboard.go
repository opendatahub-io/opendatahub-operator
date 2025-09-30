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
	// AnacondaSecretName is the name of the anaconda access secret.
	AnacondaSecretName = "anaconda-ce-access" //nolint:gosec // This is a Kubernetes secret name, not a credential
)

type ComponentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&ComponentHandler{})
}

func (s *ComponentHandler) GetName() string {
	return componentApi.DashboardComponentName
}

func (s *ComponentHandler) Init(platform common.Platform) error {
	mi := DefaultManifestInfo(platform)

	if err := odhdeploy.ApplyParams(mi.String(), "params.env", ImagesMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", mi, err)
	}

	extra := BffManifestsPath()
	if err := odhdeploy.ApplyParams(extra.String(), "params.env", ImagesMap); err != nil {
		return fmt.Errorf("failed to update %s images on path %s: %w", ModularArchitectureSourcePath, extra, err)
	}

	return nil
}

func (s *ComponentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
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

func (s *ComponentHandler) IsEnabled(dsc *dscv1.DataScienceCluster) bool {
	return dsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed
}

func (s *ComponentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	if rr.Client == nil {
		return cs, errors.New("client is nil")
	}

	if rr.DSCI == nil {
		return cs, errors.New("DSCI is nil")
	}

	dsc, ok := rr.Instance.(*dscv1.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	dashboardCRExists, c, err := s.getDashboardCR(ctx, rr)
	if err != nil {
		return cs, err
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.Dashboard.ManagementState)
	s.updateDSCStatusFields(dsc, ms)

	rr.Conditions.MarkFalse(ReadyConditionType)

	if s.IsEnabled(dsc) && dashboardCRExists {
		return s.handleEnabledDashboard(dsc, c, rr)
	}

	return s.handleDisabledDashboard(ms, rr)
}

func (s *ComponentHandler) getDashboardCR(ctx context.Context, rr *types.ReconciliationRequest) (bool, componentApi.Dashboard, error) {
	c := componentApi.Dashboard{}
	c.Name = componentApi.DashboardInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKey{Name: c.Name, Namespace: rr.DSCI.Spec.ApplicationsNamespace}, &c); err != nil {
		if k8serr.IsNotFound(err) {
			return false, c, nil
		}
		return false, c, fmt.Errorf("failed to get Dashboard CR: %w", err)
	}
	return true, c, nil
}

func (s *ComponentHandler) updateDSCStatusFields(dsc *dscv1.DataScienceCluster, ms operatorv1.ManagementState) {
	dsc.Status.InstalledComponents[LegacyComponentNameUpstream] = false
	dsc.Status.Components.Dashboard.ManagementState = ms
	dsc.Status.Components.Dashboard.DashboardCommonStatus = nil
}

func (s *ComponentHandler) handleEnabledDashboard(dsc *dscv1.DataScienceCluster, c componentApi.Dashboard, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	dsc.Status.InstalledComponents[LegacyComponentNameUpstream] = true
	dsc.Status.Components.Dashboard.DashboardCommonStatus = c.Status.DashboardCommonStatus.DeepCopy()

	if rc := conditions.FindStatusCondition(c.GetStatus(), status.ConditionTypeReady); rc != nil {
		rr.Conditions.MarkFrom(ReadyConditionType, *rc)
		return rc.Status, nil
	}
	return metav1.ConditionFalse, nil
}

func (s *ComponentHandler) handleDisabledDashboard(ms operatorv1.ManagementState, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	rr.Conditions.MarkFalse(
		ReadyConditionType,
		conditions.WithReason(string(ms)),
		conditions.WithMessage("Component ManagementState is set to %s", string(ms)),
		conditions.WithSeverity(common.ConditionSeverityInfo),
	)

	if ms == operatorv1.Managed {
		return metav1.ConditionFalse, nil
	}
	return metav1.ConditionUnknown, nil
}
