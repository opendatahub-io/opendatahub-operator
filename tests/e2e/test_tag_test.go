package e2e_test

import "testing"

type TestTag string

const (
	Smoke TestTag = "Smoke"
	Tier1 TestTag = "Tier1"
)

func skipUnless(t *testing.T, tags []TestTag) {
	// TBD
	t.Helper()
	t.Logf("Skipping test unless tags match: %v", tags)
}
