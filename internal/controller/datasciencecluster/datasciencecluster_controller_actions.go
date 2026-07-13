package datasciencecluster

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
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

func checkUpgradeGates(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	componentsEnabled := cr.DefaultRegistry().AnyComponentEnabled(instance)

	modulesEnabled := false
	if modules.DefaultRegistry().HasEntries() {
		platformCtx := &modules.PlatformContext{DSC: instance}
		modulesEnabled = modules.DefaultRegistry().AnyEnabled(platformCtx)
	}

	if !componentsEnabled && !modulesEnabled {
		return nil
	}

	return provision.CheckUpgradeGates(ctx, rr.Client, rr.Release, rr.Conditions, nil)
}

func watchDataScienceClusters(ctx context.Context, cli client.Client) []reconcile.Request {
	return cluster.WatchDataScienceClusters(ctx, cli)
}

// provisionComponents iterates over the unified DAG batches (which
// contain both components and modules) but only provisions entries of
// KindComponent. Readiness gating uses a CompositeChecker that spans
// both component and module registries, so a module that hasn't reached
// Ready blocks advancement to the next runlevel just like a component
// would.
func provisionComponents(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	checker := provision.NewCompositeChecker(
		cr.NewReadinessChecker(cr.DefaultRegistry(), rr.Client, instance),
		modules.NewReadinessChecker(modules.DefaultRegistry(), rr.Client, rr.Release.Version.String(),
			modules.WithPlatformContext(&modules.PlatformContext{DSC: instance})),
	)

	log := logf.FromContext(ctx)
	componentReg := cr.DefaultRegistry()

	var failedComponents []string

	requeueAfter, walkErr := provision.WalkBatches(ctx, checker, componentStuckTracker, string(instance.GetUID()), rr.Conditions,
		func(batch []provision.UnifiedNode) error {
			provision.GetRunlevelTracker().MarkCleared(rr.Release.Version.String(), batch[0].GetRunlevel().Order)

			for _, entry := range provision.ComponentsInBatch(batch) {
				handler := componentReg.Lookup(entry.GetName())
				if handler == nil {
					continue
				}
				if !handler.IsEnabled(instance) {
					continue
				}

				name := entry.GetName()

				ci, err := handler.NewCRObject(ctx, rr.Client, instance)
				if err != nil {
					log.Error(err, "NewCRObject failed", "component", name)
					failedComponents = append(failedComponents, name)

					continue
				}
				if isNilInterface(ci) {
					continue
				}
				obj, ok := ci.(client.Object)
				if !ok {
					log.Error(nil, "component CR does not implement client.Object",
						"component", name, "type", fmt.Sprintf("%T", ci))
					failedComponents = append(failedComponents, name)

					continue
				}
				if p, ok := ci.(persistAPI); ok {
					if inner := p.APIPersistObject(); !isNilInterface(inner) {
						obj = inner
					}
				}
				if err := rr.AddResources(obj); err != nil {
					log.Error(err, "AddResources failed", "component", name)
					failedComponents = append(failedComponents, name)

					continue
				}
			}
			return nil
		},
	)

	if walkErr != nil {
		return walkErr
	}

	if requeueAfter > 0 {
		return odherrors.NewRequeueAfterError(requeueAfter)
	}

	rr.Generated = true

	if len(failedComponents) > 0 {
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeComponentsReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.ProvisioningFailedReason,
			Message: fmt.Sprintf("Provisioning failed for: %s", strings.Join(failedComponents, ", ")),
		})

		return fmt.Errorf("provisioning failed for components: %s", strings.Join(failedComponents, ", "))
	}

	return nil
}

var componentStuckTracker = dag.NewStuckTracker()

func updateStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	instance.Status.Release = rr.Release

	if err := computeComponentsStatus(ctx, rr, cr.DefaultRegistry()); err != nil {
		return err
	}

	if cr.HasEntries() {
		if err := modules.ComputeModulesStatus(ctx, rr); err != nil {
			return err
		}
	}

	return nil
}
