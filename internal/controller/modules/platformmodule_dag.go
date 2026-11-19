package modules

import (
	"context"
	"fmt"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// coordinateDAG advances unified component/module runlevels and persists the
// durable Platform checkpoint consumed by PlatformModule controllers.
func coordinateDAG(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	platform, ok := rr.Instance.(*configApi.Platform)
	if !ok {
		return fmt.Errorf("coordinateDAG requires Platform instance, got %T", rr.Instance)
	}

	targetVersion := rr.Release.Version.String()
	if platform.Status.DAG.Release.Version.String() != targetVersion || platform.Status.DAG.Release.Name != rr.Release.Name {
		platform.Status.DAG.ClearedRunlevel = 0
	}
	platform.Status.DAG.Release = rr.Release

	checker := provision.NewCompositeChecker(
		cr.NewReadinessChecker(cr.DefaultRegistry(), rr.Client, targetVersion),
		NewReadinessChecker(DefaultRegistry(), rr.Client, &platform.Spec.Modules, targetVersion),
	)
	instanceID := string(platform.GetUID())

	requeueAfter, err := provision.WalkBatches(
		ctx,
		checker,
		moduleStuckTracker,
		instanceID,
		targetVersion,
		rr.Conditions,
		func(batch []provision.UnifiedNode) error {
			for _, node := range batch {
				if node.GetRunlevel().Order > platform.Status.DAG.ClearedRunlevel {
					platform.Status.DAG.ClearedRunlevel = node.GetRunlevel().Order
				}
			}
			return nil
		},
	)
	if err != nil {
		return err
	}
	if requeueAfter > 0 {
		return odherrors.NewRequeueAfterError(requeueAfter)
	}
	return nil
}
