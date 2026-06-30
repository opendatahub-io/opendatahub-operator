package provision

import "sync"

// RunlevelTracker records which runlevels have been cleared at a given
// operator version. The DSC controller calls MarkCleared as it walks
// the DAG; in-tree component controllers call IsCleared in their
// precondition to decide whether to proceed with reconciliation.
//
// On operator restart (including upgrades), the tracker is empty so all
// component controllers block until the DSC controller re-walks the DAG.
type RunlevelTracker struct {
	mu          sync.RWMutex
	version     string
	clearedUpTo int
}

var defaultRunlevelTracker = &RunlevelTracker{}

// GetRunlevelTracker returns the package-level singleton.
func GetRunlevelTracker() *RunlevelTracker { return defaultRunlevelTracker }

// MarkCleared records that the given runlevel order has been processed
// at the given operator version. If the version differs from what is
// stored the tracker resets, ensuring stale state from a previous
// version does not leak across upgrades.
func (t *RunlevelTracker) MarkCleared(version string, order int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.version != version {
		t.version = version
		t.clearedUpTo = order

		return
	}

	if order > t.clearedUpTo {
		t.clearedUpTo = order
	}
}

// IsCleared reports whether the given runlevel order has been reached
// at the given operator version. Returns false if the tracker has not
// been populated yet or if the version does not match.
func (t *RunlevelTracker) IsCleared(version string, order int) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.version == version && order <= t.clearedUpTo
}

// Reset clears the tracker state. Intended for testing.
func (t *RunlevelTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.version = ""
	t.clearedUpTo = 0
}
