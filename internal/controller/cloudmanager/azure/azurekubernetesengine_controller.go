package azure

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	resourceID := labels.NormalizePartOfValue(ccmv1alpha1.AzureKubernetesEngineKind)
	_, err := reconciler.ReconcilerFor(mgr, &ccmv1alpha1.AzureKubernetesEngine{}).
		WithDynamicOwnership().
		WithAction(initialize).
		ComposeWith(certmanager.Bootstrap[*ccmv1alpha1.AzureKubernetesEngine](
			ccmv1alpha1.AzureKubernetesEngineInstanceName, certmanager.DefaultBootstrapConfig())).
		WithAction(cloudmanager.NewReconcileAction(resourceID)).
		WithConditions(cloudmanager.ConditionsTypes...).
		Build(ctx)
	if err != nil {
		return err
	}

	return nil
}
