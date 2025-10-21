package skipfilter

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

// TestSkipFilter_Pass1 always passes and should be skipped in retries
func TestSkipFilter_Pass1(t *testing.T) {
	t.Log("Pass1 executed")
}

// TestSkipFilter_Pass2 always passes and should be skipped in retries
func TestSkipFilter_Pass2(t *testing.T) {
	t.Log("Pass2 executed")
}

// TestSkipFilter_Flaky fails first, passes on retry
func TestSkipFilter_Flaky(t *testing.T) {
	runCount := getAttemptCount(t)
	incrementAttemptCount(t)

	require.NotEqual(t, runCount, 0, "Flaky test failed on first run")
	resetState(t)
	t.Log("Flaky test passed on retry")
}
