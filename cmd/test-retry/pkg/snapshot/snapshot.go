package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// SnapshotTester provides snapshot testing functionality
type SnapshotTester struct {
	updateSnapshots bool
	snapshotDir     string
}

// New creates a new SnapshotTester
func New(t *testing.T) *SnapshotTester {
	t.Helper()

	// Check if we should update snapshots
	updateSnapshots := os.Getenv("UPDATE_SNAPSHOTS") == "true"

	// Create snapshots directory relative to test file
	snapshotDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		t.Fatalf("failed to create snapshot directory: %v", err)
	}

	return &SnapshotTester{
		updateSnapshots: updateSnapshots,
		snapshotDir:     snapshotDir,
	}
}

// MatchSnapshot compares the actual result with a stored snapshot
func (s *SnapshotTester) MatchSnapshot(t *testing.T, actual interface{}) {
	t.Helper()

	// Extract just the last part of the test name (without hierarchy)
	testNameParts := strings.Split(t.Name(), "/")
	testName := testNameParts[len(testNameParts)-1]

	snapshotFile := filepath.Join(s.snapshotDir, fmt.Sprintf("%s.json", testName))

	// Serialize actual result to JSON for consistent comparison
	actualJSON, err := json.MarshalIndent(actual, "", "  ")
	require.NoError(t, err, "failed to marshal actual result to JSON")

	// If updating snapshots or snapshot doesn't exist, write new snapshot
	if s.updateSnapshots || !fileExists(snapshotFile) {
		if s.updateSnapshots {
			t.Logf("Updating snapshot: %s", snapshotFile)
		} else {
			t.Logf("Creating new snapshot: %s", snapshotFile)
		}

		err := os.WriteFile(snapshotFile, actualJSON, 0644)
		require.NoError(t, err, "failed to write snapshot file")
		return
	}

	// Read existing snapshot
	expectedJSON, err := os.ReadFile(snapshotFile)
	require.NoError(t, err, "failed to read snapshot file: %s", snapshotFile)

	// Compare JSON strings for easier diff visualization
	require.JSONEq(t, string(expectedJSON), string(actualJSON),
		"Snapshot mismatch for test '%s'.\n"+
			"If this change is expected, run: UPDATE_SNAPSHOTS=true go test\n"+
			"Snapshot file: %s", testName, snapshotFile)
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
