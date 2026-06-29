package provision_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
)

func TestRunlevelTracker_Empty_NotCleared(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}
	assert.False(t, tracker.IsCleared("1.0.0", 20))
}

func TestRunlevelTracker_MarkAndCheck(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}
	tracker.MarkCleared("1.0.0", 20)

	assert.True(t, tracker.IsCleared("1.0.0", 20))
	assert.True(t, tracker.IsCleared("1.0.0", 10))
	assert.False(t, tracker.IsCleared("1.0.0", 31))
}

func TestRunlevelTracker_AdvancesMonotonically(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}
	tracker.MarkCleared("1.0.0", 20)
	tracker.MarkCleared("1.0.0", 31)

	assert.True(t, tracker.IsCleared("1.0.0", 20))
	assert.True(t, tracker.IsCleared("1.0.0", 31))
	assert.False(t, tracker.IsCleared("1.0.0", 32))
}

func TestRunlevelTracker_LowerOrderDoesNotRegress(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}
	tracker.MarkCleared("1.0.0", 31)
	tracker.MarkCleared("1.0.0", 20)

	assert.True(t, tracker.IsCleared("1.0.0", 31),
		"marking a lower order should not regress clearedUpTo")
}

func TestRunlevelTracker_VersionMismatch_NotCleared(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}
	tracker.MarkCleared("1.0.0", 31)

	assert.False(t, tracker.IsCleared("2.0.0", 20),
		"different version should not be cleared")
}

func TestRunlevelTracker_VersionChange_Resets(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}
	tracker.MarkCleared("1.0.0", 31)
	tracker.MarkCleared("2.0.0", 20)

	assert.False(t, tracker.IsCleared("1.0.0", 20),
		"old version should no longer be cleared")
	assert.True(t, tracker.IsCleared("2.0.0", 20))
	assert.False(t, tracker.IsCleared("2.0.0", 31),
		"new version should only have the newly marked order")
}

func TestRunlevelTracker_Reset(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}
	tracker.MarkCleared("1.0.0", 31)
	tracker.Reset()

	assert.False(t, tracker.IsCleared("1.0.0", 20))
}

func TestRunlevelTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	tracker := &provision.RunlevelTracker{}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)

		go func(order int) {
			defer wg.Done()

			tracker.MarkCleared("1.0.0", order)
			tracker.IsCleared("1.0.0", order)
		}(i)
	}

	wg.Wait()

	assert.True(t, tracker.IsCleared("1.0.0", 99))
}
