// This is an internal test (package cli, not cli_test) because rootOptions
// and newResolveE2EScopeCommand are both unexported, and testing the
// --config validation directly is the point.
//
//nolint:testpackage // deliberate internal test, see comment above
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveE2EScope_ConfigOutsideRepoRootIsRejected(t *testing.T) {
	dir := t.TempDir() // no .git here
	configFile := filepath.Join(dir, "manifests-config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("components: {}\n"), 0o600))

	cmd := newResolveE2EScopeCommand(&rootOptions{configFile: configFile})

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not at the repository root")
}

func TestResolveE2EScope_ConfigAtRepoRootPassesTheCheck(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	configFile := filepath.Join(dir, "manifests-config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("components: {}\n"), 0o600))

	cmd := newResolveE2EScopeCommand(&rootOptions{configFile: configFile})

	// Fails later -- this temp dir has no e2e-scope-rules.yaml or git
	// history -- but must not fail with the repo-root validation error.
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not at the repository root")
}
