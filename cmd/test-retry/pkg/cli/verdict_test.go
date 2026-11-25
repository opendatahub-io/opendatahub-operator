package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerdictCommandPreservesPolicyExitCode(t *testing.T) {
	cmd := NewVerdictCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--core-exit=0", "--dag-exit=7"})

	err := cmd.Execute()
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 7, exitErr.Code)
	assert.Contains(t, stdout.String(), "DAG failure has no trustworthy attribution")
}

func TestVerdictCommandWritesCoreOnlySummary(t *testing.T) {
	output := filepath.Join(t.TempDir(), "summary.json")
	cmd := NewVerdictCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--core-only", "--core-exit=0", "--output=" + output})

	require.NoError(t, cmd.Execute())
	assert.FileExists(t, output)
	assert.Contains(t, stdout.String(), "OPERATOR VERDICT: passed")
}

func TestExitErrorSupportsErrorsAs(t *testing.T) {
	var exitErr *ExitError
	require.True(t, errors.As(&ExitError{Code: 3}, &exitErr))
	assert.Equal(t, 3, exitErr.Code)
}
