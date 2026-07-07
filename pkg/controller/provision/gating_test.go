package provision_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
)

type readinessStub struct {
	ready map[string]bool
}

func (s *readinessStub) IsReady(_ context.Context, name string) (bool, error) {
	r, ok := s.ready[name]
	if !ok {
		return false, fmt.Errorf("unknown node %q: %w", name, dag.ErrUnknownNode)
	}
	return r, nil
}

type conditionRecorder struct {
	conditions []common.Condition
}

func (c *conditionRecorder) SetCondition(cond common.Condition) {
	c.conditions = append(c.conditions, cond)
}

func (c *conditionRecorder) last() common.Condition {
	return c.conditions[len(c.conditions)-1]
}

func (c *conditionRecorder) byReason(reason string) []common.Condition {
	var result []common.Condition
	for _, cond := range c.conditions {
		if cond.Reason == reason {
			result = append(result, cond)
		}
	}
	return result
}

func resetDefaultRegistry(t *testing.T, entries map[string]dag.Runlevel) {
	t.Helper()
	r := provision.DefaultRegistry()
	r.Reset()
	t.Cleanup(func() { r.Reset() })
	for name, rl := range entries {
		r.Add(name, provision.KindComponent, rl)
	}
}

func TestWalkBatches_AllReady_NoGating(t *testing.T) {
	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha": dag.RL(20),
		"beta":  dag.RL(31),
	})

	checker := &readinessStub{ready: map[string]bool{"alpha": true, "beta": true}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}
	var processed []string

	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		for _, n := range batch {
			processed = append(processed, n.GetName())
		}
		return nil
	})

	require.NoError(t, err)
	assert.Zero(t, requeueAfter)
	assert.Equal(t, []string{"alpha", "beta"}, processed)
	assert.Equal(t, status.ConditionTypeProvisioningProgress, conds.last().Type)
	assert.Equal(t, "True", string(conds.last().Status))
}

func TestWalkBatches_GatingBlocks(t *testing.T) {
	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha": dag.RL(20),
		"beta":  dag.RL(31),
	})

	checker := &readinessStub{ready: map[string]bool{"alpha": false, "beta": true}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}
	var processed []string

	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		for _, n := range batch {
			processed = append(processed, n.GetName())
		}
		return nil
	})

	require.NoError(t, err)
	assert.Positive(t, requeueAfter, "should request requeue for remaining timeout")
	assert.Equal(t, []string{"alpha"}, processed, "beta should be gated")
	assert.Equal(t, status.AwaitingReadinessReason, conds.last().Reason)
}

func TestWalkBatches_GatingBlocks_RequeueDurationMatchesRemainingTimeout(t *testing.T) {
	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha": dag.RL(20),
		"beta":  dag.RL(31),
	})

	customTimeout := 5 * time.Minute
	dag.SetRunlevelPolicy(31, dag.RunlevelPolicy{Timeout: customTimeout})
	defer dag.ClearRunlevelPolicy(31)

	checker := &readinessStub{ready: map[string]bool{"alpha": false, "beta": true}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}

	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		return nil
	})

	require.NoError(t, err)
	assert.InDelta(t, customTimeout.Seconds(), requeueAfter.Seconds(), 1.0,
		"requeue duration should be close to the full timeout on first observation")
	assert.Equal(t, status.AwaitingReadinessReason, conds.last().Reason)
}

func TestWalkBatches_TimeoutAdvancesPastStuckRunlevel(t *testing.T) {
	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha": dag.RL(20),
		"beta":  dag.RL(31),
	})

	dag.SetRunlevelPolicy(31, dag.RunlevelPolicy{Timeout: 1 * time.Millisecond})
	defer dag.ClearRunlevelPolicy(31)

	checker := &readinessStub{ready: map[string]bool{"alpha": false, "beta": true}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}

	tracker.Since("test", 31)
	time.Sleep(2 * time.Millisecond)

	var processed []string
	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		for _, n := range batch {
			processed = append(processed, n.GetName())
		}
		return nil
	})

	require.NoError(t, err)
	assert.Zero(t, requeueAfter, "timeout already fired, no requeue needed")
	assert.Equal(t, []string{"alpha", "beta"}, processed, "beta should be processed after timeout")
	skipped := conds.byReason(status.RunlevelTimeoutExceededReason)
	require.Len(t, skipped, 1)
	assert.Contains(t, skipped[0].Message, "alpha")
}

func TestWalkBatches_TimeoutPropagates_NoReGating(t *testing.T) {
	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"dashboard": dag.RL(20),
		"kserve":    dag.RL(31),
		"trustyai":  dag.RL(33),
	})

	dag.SetRunlevelPolicy(31, dag.RunlevelPolicy{Timeout: 1 * time.Millisecond})
	defer dag.ClearRunlevelPolicy(31)

	checker := &readinessStub{ready: map[string]bool{
		"dashboard": false,
		"kserve":    true,
		"trustyai":  true,
	}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}

	tracker.Since("test", 31)
	time.Sleep(2 * time.Millisecond)

	var processed []string
	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		for _, n := range batch {
			processed = append(processed, n.GetName())
		}
		return nil
	})

	require.NoError(t, err)
	assert.Zero(t, requeueAfter, "timeout already fired, no requeue needed")
	assert.Equal(t, []string{"dashboard", "kserve", "trustyai"}, processed,
		"once dashboard times out at RL31, RL33 should not re-gate on it")
}

func TestWalkBatches_TimeoutDoesNotForgiveNewStuckEntry(t *testing.T) {
	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha":   dag.RL(20),
		"bravo":   dag.RL(31),
		"charlie": dag.RL(33),
	})

	dag.SetRunlevelPolicy(31, dag.RunlevelPolicy{Timeout: 1 * time.Millisecond})
	defer dag.ClearRunlevelPolicy(31)

	checker := &readinessStub{ready: map[string]bool{
		"alpha":   false,
		"bravo":   false,
		"charlie": true,
	}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}

	tracker.Since("test", 31)
	time.Sleep(2 * time.Millisecond)

	var processed []string
	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		for _, n := range batch {
			processed = append(processed, n.GetName())
		}
		return nil
	})

	require.NoError(t, err)
	assert.Positive(t, requeueAfter, "bravo is newly stuck, should request requeue")
	assert.Equal(t, []string{"alpha", "bravo"}, processed,
		"alpha timed out at RL31, but bravo is newly stuck at RL33 and should gate charlie")
	assert.Equal(t, status.AwaitingReadinessReason, conds.last().Reason,
		"should be awaiting readiness on bravo, not skipped")
}

func TestWalkBatches_DAGOrderingDisabled_BypassesGating(t *testing.T) {
	viper.Set("disable-dag-ordering", true)
	t.Cleanup(func() { viper.Set("disable-dag-ordering", false) })

	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha": dag.RL(20),
		"beta":  dag.RL(31),
	})

	// alpha not ready — would normally block beta
	checker := &readinessStub{ready: map[string]bool{"alpha": false, "beta": true}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}
	var processed []string

	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		for _, n := range batch {
			processed = append(processed, n.GetName())
		}
		return nil
	})

	require.NoError(t, err)
	assert.Zero(t, requeueAfter, "no requeue when gating is disabled")
	assert.Equal(t, []string{"alpha", "beta"}, processed, "all batches processed regardless of readiness")
}

func TestWalkBatches_DAGOrderingEnabled_StillGates(t *testing.T) {
	viper.Set("disable-dag-ordering", false)
	t.Cleanup(func() { viper.Set("disable-dag-ordering", false) })

	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha": dag.RL(20),
		"beta":  dag.RL(31),
	})

	checker := &readinessStub{ready: map[string]bool{"alpha": false, "beta": true}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}
	var processed []string

	requeueAfter, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		for _, n := range batch {
			processed = append(processed, n.GetName())
		}
		return nil
	})

	require.NoError(t, err)
	assert.Positive(t, requeueAfter, "should requeue while alpha is not ready")
	assert.Equal(t, []string{"alpha"}, processed, "beta should be gated by non-ready alpha")
}

func TestWalkBatches_ProcessBatchErrorHaltsWalk(t *testing.T) {
	resetDefaultRegistry(t, map[string]dag.Runlevel{
		"alpha": dag.RL(20),
		"beta":  dag.RL(31),
	})

	checker := &readinessStub{ready: map[string]bool{"alpha": true, "beta": true}}
	tracker := dag.NewStuckTracker()
	conds := &conditionRecorder{}

	_, err := provision.WalkBatches(context.Background(), checker, tracker, "test", conds, func(batch []provision.UnifiedNode) error {
		return errors.New("reconcile failed")
	})

	require.ErrorContains(t, err, "reconcile failed")
}
