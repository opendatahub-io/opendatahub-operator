package flaky

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func getStateFile(t *testing.T) string {
	return "./" + t.Name() + ".txt"
}

func getAttemptCount(t *testing.T) int {
	data, err := os.ReadFile(getStateFile(t))
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
	count := getAttemptCount(t)
	err := os.WriteFile(getStateFile(t), []byte(fmt.Sprintf("%d", count+1)), 0644)
	require.NoError(t, err)
}

func resetState(t *testing.T) {
	os.Remove(getStateFile(t))
}

// TestFlaky1 fails on first run, passes on subsequent runs
func TestFlaky1(t *testing.T) {
	attempt := getAttemptCount(t)
	incrementAttemptCount(t)

	require.NotEqual(t, attempt, 0, "Flaky test 1 failed on first attempt")
	resetState(t)
	t.Log("Flaky test 1 passed on retry")
}

// TestFlaky2 fails on first run, passes on subsequent runs
func TestFlaky2(t *testing.T) {
	attempt := getAttemptCount(t)
	incrementAttemptCount(t)

	require.NotEqual(t, attempt, 0, "Flaky test 2 failed on first attempt")
	resetState(t)
	t.Log("Flaky test 2 passed on retry")
}

// TestNonFlaky always passes
func TestNonFlaky(t *testing.T) {
	t.Log("Non-flaky test passed")
}
