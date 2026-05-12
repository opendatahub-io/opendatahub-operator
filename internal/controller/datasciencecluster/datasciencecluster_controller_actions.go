package datasciencecluster

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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

// persistAPI is implemented by component CRs that expose an alternative object
// for the deploy action to persist (e.g. when the "public" CR wraps an inner
// object that should actually be applied to the cluster).
type persistAPI interface {
	APIPersistObject() client.Object
}

func isNilInterface(v any) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil())
}

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
		if isNilInterface(ci) {
			return nil
		}
		obj, ok := ci.(client.Object)
		if !ok {
			return fmt.Errorf("component CR %T does not implement client.Object", ci)
		}
		if p, ok := ci.(persistAPI); ok {
			if inner := p.APIPersistObject(); !isNilInterface(inner) {
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
		// maas-controller resources (CRDs, RBAC, Deployment); Tenant/platform reconcile stays in maas-controller.
		if err := modelsasservicectrl.AppendOperatorInstallManifests(ctx, rr); err != nil {
			return err
		}
	}

	if err := deleteMaaSDeploymentIfDisabled(ctx, rr, instance, cr.DefaultRegistry()); err != nil {
		return err
	}

	return nil
}

// deleteMaaSDeploymentIfDisabled deletes the maas-controller Deployment when
// MaaS is disabled. The CleanupFinalizer on the Deployment causes
// LifecycleReconciler to drain Tenant CRs, RBAC, and CRDs before the
// Deployment object is removed.
func deleteMaaSDeploymentIfDisabled(
	ctx context.Context,
	rr *odhtype.ReconciliationRequest,
	dsc *dscv2.DataScienceCluster,
	reg *cr.Registry,
) error {
	log := ctrl.LoggerFrom(ctx)

	if reg.IsComponentEnabled(componentApi.ModelsAsServiceComponentName, dsc) {
		return nil
	}

	appNs, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("get application namespace for maas-controller cleanup: %w", err)
	}

	dep := &appsv1.Deployment{}
	err = rr.Client.Get(ctx, types.NamespacedName{Name: "maas-controller", Namespace: appNs}, dep)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("get maas-controller Deployment: %w", err)
	}

	// Deployment still exists — ensure deletion is requested.
	// LifecycleReconciler's CleanupFinalizer will drain Tenants, RBAC, and CRDs
	// before the Deployment object is removed.
	if dep.DeletionTimestamp.IsZero() {
		if err := rr.Client.Delete(ctx, dep); err != nil && !k8serr.IsNotFound(err) {
			return fmt.Errorf("delete maas-controller Deployment: %w", err)
		}
	}
	log.Info("maas-controller Deployment is terminating; waiting for CleanupFinalizer to complete")
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
