package e2e_test

import (
	"slices"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/tests/tagging"
)

type TestTag string

const (
	All   TestTag = "All"
	Smoke TestTag = "Smoke"
	Tier1 TestTag = "Tier1"
	Tier2 TestTag = "Tier2"
	Tier3 TestTag = "Tier3"
)

var allowedTags = []TestTag{All, Smoke, Tier1, Tier2, Tier3}

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
