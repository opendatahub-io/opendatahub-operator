package servicemesh

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&serviceHandler{})
}

type serviceHandler struct {
}

func (h *serviceHandler) Init(_ common.Platform) error {
	return nil
}

func (h *serviceHandler) GetName() string {
	return ServiceName
}

func (h *serviceHandler) GetManagementState(_ common.Platform, _ *dsciv1.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

func (h *serviceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.ServiceMesh{}).
		// monitoring-related resources
		OwnsGVK(gvk.PodMonitorServiceMesh,
			reconciler.Dynamic(actions.IfGVKInstalled(gvk.PodMonitorServiceMesh))).
		OwnsGVK(gvk.ServiceMonitorServiceMesh,
			reconciler.Dynamic(actions.IfGVKInstalled(gvk.ServiceMonitorServiceMesh))).
		// authorino-related resources
		OwnsGVK(gvk.ServiceMeshMember,
			reconciler.Dynamic(actions.IfGVKInstalled(gvk.ServiceMeshMember))).
		OwnsGVK(gvk.Authorino,
			reconciler.Dynamic(actions.IfGVKInstalled(gvk.Authorino))).
		Owns(&appsv1.Deployment{},
			reconciler.WithPredicates(NewAuthorinoDeploymentPredicate())).
		// watch for SMCP readiness
		WatchesGVK(gvk.ServiceMeshControlPlane,
			reconciler.Dynamic(actions.IfGVKInstalled(gvk.ServiceMeshControlPlane)),
			reconciler.WithPredicates(NewSMCPReadyPredicate()),
		).
		WithAction(checkPreconditions).
		WithAction(createControlPlaneNamespace).
		WithAction(initializeServiceMesh).
		WithAction(initializeServiceMeshMetricsCollection).
		WithAction(initializeAuthorino).
		WithAction(template.NewAction(
			template.WithDataFn(getTemplateData),
		)).
		WithAction(updateMeshRefsConfigMap).
		WithAction(updateAuthRefsConfigMap).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deleteFeatureTrackers).
		WithAction(checkSMCPReadiness).
		WithAction(checkAuthorinoReadiness).
		// can't own SMCP directly due to conflicts with ServiceMesh v2 operator
		// but SMCP created by ODH operator will be cleaned up via this finalizer
		WithFinalizer(cleanupSMCP).
		WithConditions(conditionTypes...).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create ServiceMesh controller: %w", err)
	}

	return nil
}
