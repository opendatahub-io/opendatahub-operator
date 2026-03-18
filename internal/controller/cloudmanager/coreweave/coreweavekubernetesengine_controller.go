package coreweave

import (
	"context"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	resourceID := labels.NormalizePartOfValue(ccmv1alpha1.CoreWeaveKubernetesEngineKind)
	_, err := reconciler.ReconcilerFor(mgr, &ccmv1alpha1.CoreWeaveKubernetesEngine{}).
		WithDynamicOwnership().
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(handlers.ToNamed(ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName)),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed(common.ServiceMonitorCRDName)),
		).
		WithAction(initialize).
		ComposeWith(certmanager.Bootstrap[*ccmv1alpha1.CoreWeaveKubernetesEngine](
			ccmv1alpha1.CoreWeaveKubernetesEngineInstanceName,
			certmanager.DefaultBootstrapConfig(certmanager.WithOperatorCert()),
		)).
		WithActionE(cloudmanager.NewReconcileAction(resourceID)).
		WithConditions(cloudmanager.ConditionsTypes...).
		Build(ctx)
	if err != nil {
		return err
	}

	return nil
}
