package datasciencecluster

import (
	"context"
	"fmt"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	modelsasservicectrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelsasservice"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// TODO: remove after https://issues.redhat.com/browse/RHOAIENG-15920
	finalizerName = "datasciencecluster.opendatahub.io/finalizer"
)

func initialize(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	// TODO: remove after https://issues.redhat.com/browse/RHOAIENG-15920
	if controllerutil.RemoveFinalizer(instance, finalizerName) {
		if err := rr.Client.Update(ctx, instance); err != nil {
			return err
		}
	}

	return nil
}

func checkPreConditions(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	// This case should not happen, since there is a webhook that blocks the creation
	// of more than one instance of the DataScienceCluster, however one can create a
	// DataScienceCluster instance while the operator is stopped, hence this extra check

	if _, err := cluster.GetDSCI(ctx, rr.Client); err != nil {
		return fmt.Errorf("failed to get a valid DataScienceCluster instance, %w", err)
	}

	if _, err := cluster.GetDSC(ctx, rr.Client); err != nil {
		return fmt.Errorf("failed to get a valid DSCInitialization instance, %w", err)
	}

	return nil
}

func watchDataScienceClusters(ctx context.Context, cli client.Client) []reconcile.Request {
	instanceList := &dscv2.DataScienceClusterList{}
	err := cli.List(ctx, instanceList)
	if err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(instanceList.Items))
	for i := range instanceList.Items {
		requests[i] = reconcile.Request{NamespacedName: types.NamespacedName{Name: instanceList.Items[i].Name}}
	}

	return requests
}

func provisionComponents(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	// force gc to run
	rr.Generated = true

	// ForEach continues on component errors; all enabled components are
	// still provisioned, but any error causes this reconcile to fail and retry.
	err := cr.ForEach(func(component cr.ComponentHandler) error {
		if !component.IsEnabled(instance) {
			return nil
		}

		ci, err := component.NewCRObject(ctx, rr.Client, instance)
		if err != nil {
			return err
		}
		if ci == nil {
			return nil
		}
		obj, ok := ci.(client.Object)
		if !ok {
			return fmt.Errorf("component CR %T does not implement client.Object", ci)
		}
		type persistAPI interface {
			APIPersistObject() client.Object
		}
		if p, ok := ci.(persistAPI); ok {
			if inner := p.APIPersistObject(); inner != nil {
				obj = inner
			}
		}
		if err := rr.AddResources(obj); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	if cr.DefaultRegistry().IsComponentEnabled(componentApi.ModelsAsServiceComponentName, instance) {
		// maas-controller install bundle (CRDs, RBAC, Deployment); Tenant/platform reconcile stays in maas-controller.
		if err := modelsasservicectrl.AppendOperatorInstallManifests(ctx, rr); err != nil {
			return err
		}
	}

	if err := removeTenantIfModelsAsServiceDisabled(ctx, rr, instance, cr.DefaultRegistry()); err != nil {
		return err
	}

	return nil
}

// removeTenantIfModelsAsServiceDisabled deletes the DSC-managed singleton Tenant when
// Models-as-a-Service is not enabled. Deploy only applies objects in rr.Resources, so omitting
// the CR on disable does not remove it; this keeps cluster state aligned with DSC intent.
func removeTenantIfModelsAsServiceDisabled(
	ctx context.Context,
	rr *odhtype.ReconciliationRequest,
	dsc *dscv2.DataScienceCluster,
	reg *cr.Registry,
) error {
	if reg.IsComponentEnabled(componentApi.ModelsAsServiceComponentName, dsc) {
		return nil
	}

	key := client.ObjectKey{Name: maasv1alpha1.TenantInstanceName, Namespace: modelsasservicectrl.MaaSSubscriptionNamespace}
	t := &maasv1alpha1.Tenant{}
	if err := rr.Client.Get(ctx, key, t); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("get Tenant %s: %w", maasv1alpha1.TenantInstanceName, err)
	}

	// Already being finalized; no need to re-issue the delete.
	if !t.GetDeletionTimestamp().IsZero() {
		return nil
	}

	if err := rr.Client.Delete(ctx, t); err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("delete Tenant %s when ModelsAsService disabled: %w", maasv1alpha1.TenantInstanceName, err)
	}

	return nil
}

func updateStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	instance.Status.Release = rr.Release

	err := computeComponentsStatus(ctx, rr, cr.DefaultRegistry())
	if err != nil {
		return err
	}

	return nil
}
