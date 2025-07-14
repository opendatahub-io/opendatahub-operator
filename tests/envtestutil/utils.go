package envtestutil

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func FindProjectRoot() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(currentDir, "go.mod")); err == nil {
			return filepath.FromSlash(currentDir), nil
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}

		currentDir = parentDir
	}

	return "", errors.New("project root not found")
}

func JSONMarshal(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err, "Failed to marshal JSON")
	return string(b)
}
