package e2e_test

import (
	"slices"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/tests/tagging"
)

type TestTag string

const (
	All         TestTag = "All"
	Smoke       TestTag = "Smoke"
	Tier1       TestTag = "Tier1"
	Tier2       TestTag = "Tier2"
	Tier3       TestTag = "Tier3"
	PreUpgrade  TestTag = "PreUpgrade"
	PostUpgrade TestTag = "PostUpgrade"
)

var allowedTags = []TestTag{All, Smoke, Tier1, Tier2, Tier3, PreUpgrade, PostUpgrade}

// skipUnlessUpgradePhase runs the test only when the exact upgrade-phase tag
// is selected. Unlike skipUnless, tag "All" does not run upgrade-phase tests:
// post-upgrade assertions are meaningless without a pre-upgrade baseline.
func skipUnlessUpgradePhase(t *testing.T, phase TestTag) {
	t.Helper()
	if TestTag(testOpts.tag) != phase {
		t.Skipf("%s %s (upgrade phases run only under their exact tag)", tagging.GateSkipPrefix, testOpts.tag)
	}
}

func skipUnless(t *testing.T, tags ...TestTag) {
	t.Helper()
	// if the 'All' tag is selected, return early to run all tests
	if TestTag(testOpts.tag) == All {
		return
	}
	skipTest := !slices.Contains(tags, TestTag(testOpts.tag))

	if skipTest {
		t.Skipf("%s %s, test tags: %v", tagging.GateSkipPrefix, testOpts.tag, tags)
	}
}
