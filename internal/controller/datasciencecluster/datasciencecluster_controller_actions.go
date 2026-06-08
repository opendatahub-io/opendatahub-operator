package datasciencecluster

import (
	"context"
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
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
	return cluster.WatchDataScienceClusters(ctx, cli)
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
