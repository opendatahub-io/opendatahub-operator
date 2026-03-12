package coreweave

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	resourceID := labels.NormalizePartOfValue(ccmv1alpha1.CoreWeaveKubernetesEngineKind)
	_, err := reconciler.ReconcilerFor(mgr, &ccmv1alpha1.CoreWeaveKubernetesEngine{}).
		WithDynamicOwnership().
		WithAction(initialize).
		ComposeWith(certmanager.Bootstrap[*ccmv1alpha1.CoreWeaveKubernetesEngine](
			ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName, certmanager.DefaultBootstrapConfig())).
		WithAction(cloudmanager.NewReconcileAction(resourceID)).
		WithConditions(cloudmanager.ConditionsTypes...).
		Build(ctx)
	if err != nil {
		return err
	}

	return nil
}
