package cli

import (
	"os"
	"testing"
	"path/filepath"
	"github.com/stretchr/testify/require"
)

func TestFilterGateSkipsCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "junit.xml")
	require.NoError(t, os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="2" failures="0" skipped="1">
  <testsuite name="odh-operator-e2e" tests="2" failures="0" skipped="1">
    <testcase name="gate" time="0.001">
      <skipped message="Skipping test: passed tag: Smoke, test tags: [Tier2]"></skipped>
    </testcase>
    <testcase name="ok" time="0.1"></testcase>
  </testsuite>
</testsuites>
`), 0o644))

	cmd := NewFilterGateSkipsCommand()
	cmd.SetArgs([]string{"--junit", path})
	require.NoError(t, cmd.Execute())

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(content), "Skipping test: passed tag:")
	require.Contains(t, string(content), `name="ok"`)


}