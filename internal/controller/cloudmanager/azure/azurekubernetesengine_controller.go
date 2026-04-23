package azure

import (
	"context"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

// NewReconciler sets up the AzureKubernetesEngine controller and registers it with the manager.
func NewReconciler(ctx context.Context, mgr ctrl.Manager, cfg *operatorconfig.CloudManagerConfig) error {
	resourceID := labels.NormalizePartOfValue(ccmv1alpha1.AzureKubernetesEngineKind)
	bootstrapConfig := certmanager.DefaultBootstrapConfig(certmanager.WithOperatorCert(cfg.RhaiOperatorNamespace))

	_, err := reconciler.ReconcilerFor(mgr, &ccmv1alpha1.AzureKubernetesEngine{}).
		WithDynamicOwnership().
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(handlers.ToNamed(ccmv1alpha1.AzureKubernetesEngineInstanceName)),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed(common.ServiceMonitorCRDName)),
		).
		WithAction(initialize).
		ComposeWith(certmanager.Bootstrap[*ccmv1alpha1.AzureKubernetesEngine](
			ccmv1alpha1.AzureKubernetesEngineInstanceName,
			bootstrapConfig,
		)).
		WithActionE(cloudmanager.NewReconcileAction(resourceID)).
		// GC must be last: evaluates every CCM resource and removes stale or orphaned ones.
		WithActionE(cloudmanager.NewGCAction(resourceID, cfg.RhaiOperatorNamespace,
			cloudmanager.BootstrapProtectedObjects(bootstrapConfig),
		)).
		WithConditions(cloudmanager.ConditionsTypes...).
		Build(ctx)
	return err
}
