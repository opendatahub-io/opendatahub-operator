package provision

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

// ConditionWriter is the subset of conditions.Manager that the gating
// walker needs. Using an interface keeps this package decoupled from the
// concrete Manager type.
type ConditionWriter interface {
	SetCondition(cond common.Condition)
}

// NoOpConditionWriter is a ConditionWriter that discards all writes.
// Use when a caller needs DAG gating logic but should not own the
// ProvisioningProgress condition (e.g. the modules controller, which
// defers condition ownership to the DSC controller).
type NoOpConditionWriter struct{}

func (NoOpConditionWriter) SetCondition(common.Condition) {}

// BatchProcessor is called for each batch that passes readiness gating.
type BatchProcessor func(batch []UnifiedNode) error

// WalkBatches resolves the unified DAG and iterates batches in runlevel
// order, enforcing readiness gating between runlevels. For each batch
// whose prior runlevels are all ready, processBatch is called. Timeout
// and waiting conditions are written to ProvisioningProgress.
//
// Returns the remaining duration until the blocking runlevel's timeout
// expires (zero when not blocked or already timed out) and any error.
// Callers should schedule a requeue for the returned duration so the
// timeout check fires even without external events.
func WalkBatches(
	ctx context.Context,
	checker dag.ReadinessChecker,
	tracker *dag.StuckTracker,
	instanceID string,
	conditions ConditionWriter,
	processBatch BatchProcessor,
) (time.Duration, error) {
	log := logf.FromContext(ctx)

	batches, err := DefaultRegistry().ResolvedBatches()
	if err != nil {
		conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeProvisioningProgress,
			Status:  metav1.ConditionFalse,
			Reason:  status.DAGResolutionFailedReason,
			Message: fmt.Sprintf("Unified DAG resolution failed: %v", err),
		})
		return 0, fmt.Errorf("unified DAG resolution failed: %w", err)
	}

	progressBlocked := false
	timedOut := map[string]bool{}

	var requeueAfter time.Duration

	for batchIdx, batch := range batches {
		if batchIdx > 0 && !flags.IsDAGOrderingDisabled() {
			allReady := true
			var notReadyInPrev []string

			for _, prevBatch := range batches[:batchIdx] {
				for _, entry := range prevBatch {
					if timedOut[entry.GetName()] {
						log.V(1).Info("skipping previously timed-out entry in readiness check",
							"entry", entry.GetName(),
							"runlevel", batch[0].GetRunlevel(),
						)
						continue
					}
					ready, err := checker.IsReady(ctx, entry.GetName())
					if err != nil {
						log.Error(err, "readiness check failed, treating as not ready", "name", entry.GetName())
					}
					if !ready {
						allReady = false
						notReadyInPrev = append(notReadyInPrev, entry.GetName())
					}
				}
			}

			if !allReady {
				currentOrder := batch[0].GetRunlevel().Order
				policy := dag.GetRunlevelPolicy(currentOrder)
				stuckSince := tracker.Since(instanceID, currentOrder)
				elapsed := time.Since(stuckSince)

				progressBlocked = true

				if policy.Timeout > 0 && elapsed >= policy.Timeout {
					log.Info("runlevel timeout exceeded, advancing past stuck entries",
						"runlevel", batch[0].GetRunlevel(),
						"elapsed", elapsed.Truncate(time.Second),
						"timeout", policy.Timeout,
						"not_ready", notReadyInPrev,
					)
					conditions.SetCondition(common.Condition{
						Type:    status.ConditionTypeProvisioningProgress,
						Status:  metav1.ConditionFalse,
						Reason:  status.RunlevelTimeoutExceededReason,
						Message: fmt.Sprintf("Timed out after %s; not ready: %s", dag.FormatDuration(policy.Timeout), strings.Join(notReadyInPrev, ", ")),
					})

					for _, name := range notReadyInPrev {
						timedOut[name] = true
					}

					batchNames := make([]string, len(batch))
					for i, n := range batch {
						batchNames[i] = n.GetName()
					}
					log.Info("proceeding with batch despite timed-out prior entries",
						"runlevel", batch[0].GetRunlevel(),
						"batch_entries", batchNames,
						"timed_out_entries", notReadyInPrev,
					)
				} else {
					remaining := policy.Timeout - elapsed
					log.Info("runlevel gating: waiting for prior entries",
						"runlevel", batch[0].GetRunlevel(),
						"blocked_on", notReadyInPrev,
						"elapsed", elapsed.Truncate(time.Second),
						"timeout", policy.Timeout,
						"requeue_after", remaining.Truncate(time.Second),
					)

					conditions.SetCondition(common.Condition{
						Type:    status.ConditionTypeProvisioningProgress,
						Status:  metav1.ConditionFalse,
						Reason:  status.AwaitingReadinessReason,
						Message: fmt.Sprintf("Waiting up to %s on %s", dag.FormatDuration(policy.Timeout), strings.Join(notReadyInPrev, ", ")),
					})

					requeueAfter = remaining

					break
				}
			}
		}

		if err := processBatch(batch); err != nil {
			return 0, err
		}

		if batchIdx > 0 {
			tracker.Clear(instanceID, batch[0].GetRunlevel().Order)
		}
	}

	if !progressBlocked {
		conditions.SetCondition(common.Condition{
			Type:   status.ConditionTypeProvisioningProgress,
			Status: metav1.ConditionTrue,
		})
	}

	return requeueAfter, nil
}
