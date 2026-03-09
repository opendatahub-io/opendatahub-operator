package e2e_test

import "testing"

type TestTag string

const (
	All   TestTag = "All"
	Smoke TestTag = "Smoke"
	Tier1 TestTag = "Tier1"
)

var allowedTags = []TestTag{All, Smoke, Tier1}

func skipUnless(t *testing.T, tags ...TestTag) {
	t.Helper()
	// if the 'All' tag is selected, return early to run all tests
	if TestTag(testOpts.tag) == All {
		return
	}
	skipTest := true
	for _, tag := range tags {
		if tag == TestTag(testOpts.tag) {
			skipTest = false
			break
		}
	}

	if skipTest {
		t.Skipf("Skipping test: passed tag: %s, test tags: %v", testOpts.tag, tags)
	}
}
