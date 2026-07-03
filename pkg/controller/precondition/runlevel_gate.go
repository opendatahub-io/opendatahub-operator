package precondition

import (
	"context"
	"fmt"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

const PlatformReadyConditionType = "PlatformReady"

// RunlevelGateAction returns an action that checks whether the platform
// orchestrator has reached this component's runlevel. When the runlevel
// has not been cleared, it sets rr.SkipDeploy so that render/deploy/GC
// actions become no-ops while status-reporting actions continue to run.
//
// PlatformReady is an informational condition (Info severity) that does
// not affect the component's Ready status.
func RunlevelGateAction() actions.Fn {
	return runlevelGateAction
}

func runlevelGateAction(ctx context.Context, rr *types.ReconciliationRequest) error {
	if flags.IsDAGOrderingDisabled() {
		return nil
	}

	kind := rr.Instance.GetObjectKind().GroupVersionKind().Kind
	componentName := strings.ToLower(kind)

	order, found := provision.DefaultRegistry().LookupOrder(componentName)
	if !found {
		rr.Conditions.MarkTrue(PlatformReadyConditionType,
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)

		return nil
	}

	version := rr.Release.Version.String()
	if !provision.GetRunlevelTracker().IsCleared(version, order) {
		rr.SkipDeploy = true

		msg := fmt.Sprintf("provisioning order %d not yet reached at version %s; waiting for platform orchestrator", order, version)
		logf.FromContext(ctx).Info("Runlevel not cleared, skipping deploy", "order", order, "version", version)

		rr.Conditions.MarkFalse(PlatformReadyConditionType,
			conditions.WithReason("RunlevelNotCleared"),
			conditions.WithMessage("%s", msg),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)

		// Requeue so the controller rechecks when the tracker advances.
		return odherrors.NewRequeueAfterError(30 * time.Second)
	}

	rr.Conditions.MarkTrue(PlatformReadyConditionType,
		conditions.WithSeverity(common.ConditionSeverityInfo),
	)

	return nil
}
