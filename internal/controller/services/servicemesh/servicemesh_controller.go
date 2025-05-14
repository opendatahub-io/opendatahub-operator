package servicemesh

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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
		// OwnsGVK(gvk.ServiceMeshControlPlane, reconciler.WithPredicates(smcpReadyPredicate)).
		// TODO fix - SMCP instance (data-science-smcp) should be owned by ServiceMesh (previously owned by FeatureTracker)
		// currently, SMCP instance does not reach ready state:
		// getting Dependency "Jaeger CRD" is missing: error: no matches for kind "Jaeger" in version "jaegertracing.io/v1"
		// none of this seems to happen with previous implementation that uses FeatureTrackers
		// the difference between implementations - FeatureTrackers used to wait for Pods/SMCP to become ready
		OwnsGVK(gvk.ServiceMeshMember).
		OwnsGVK(gvk.PodMonitor).
		OwnsGVK(gvk.ServiceMonitor).
		OwnsGVK(gvk.Authorino).
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
		// TODO: can be removed after RHOAI 2.26 (next EUS)
		WithAction(deleteFeatureTrackers).
		WithAction(updateStatus).
		WithConditions(conditionTypes...).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create ServiceMesh controller: %w", err)
	}

	return nil
}
