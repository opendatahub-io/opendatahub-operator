package flaky

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func getStateFile(name string) string {
	return "./" + name + ".txt"
}

func getAttemptCount(name string) int {
	data, err := os.ReadFile(getStateFile(name))
	if err != nil {
		return 0
	}

	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return v
}

func incrementAttemptCount(t *testing.T) {
	count := getAttemptCount(t.Name())
	err := os.WriteFile(getStateFile(t.Name()), []byte(fmt.Sprintf("%d", count+1)), 0644)
	require.NoError(t, err)
}

func resetState(t *testing.T) {
	os.Remove(getStateFile(t.Name()))
}

func runFlakyTest(t *testing.T) bool {
	attempt := getAttemptCount("TestFlaky3")
	if attempt > 1 {
		return true
	}
	return false
}

// TestFlaky1 fails on first run, passes on subsequent runs
func TestFlaky1(t *testing.T) {
	if runFlakyTest(t) {
		t.Log("Skip flaky test 1")
		return
	}
	attempt := getAttemptCount(t.Name())
	incrementAttemptCount(t)

	require.NotEqual(t, attempt, 0, "Flaky test 1 failed on first attempt")
	resetState(t)
	t.Log("Flaky test 1 passed on retry")
}

// TestFlaky2 fails on first run, passes on subsequent runs
func TestFlaky2(t *testing.T) {
	if runFlakyTest(t) {
		t.Log("Skip flaky test 2")
		return
	}
	attempt := getAttemptCount(t.Name())
	incrementAttemptCount(t)

	require.NotEqual(t, attempt, 0, "Flaky test 2 failed on first attempt")
	resetState(t)
	t.Log("Flaky test 2 passed on retry")
}

// TestFlaky3 fails on first and second run, passes on subsequent runs
func TestFlaky3(t *testing.T) {
	attempt := getAttemptCount(t.Name())
	incrementAttemptCount(t)

	require.Greater(t, attempt, 1, "Flaky test 3 failed on first attempt")
	resetState(t)
}

// TestNonFlaky always passes
func TestNonFlaky(t *testing.T) {
	t.Log("Non-flaky test passed")
}
