// This is an internal test (package e2escope, not e2escope_test) because it
// reuses initGitRepo/runGitForTest from git_test.go, in this same package.
//
//nolint:testpackage // deliberate internal test, see comment above
package e2escope

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const illegalImageOverrideKeyYAML = `
components:
  kserve:
    odh:
      repo: opendatahub-io/example
      ref: main@aaa
      sourcePath: config
imageOverrides:
  NOT_A_RELATED_IMAGE:
    component: kserve
`

func TestResolveManifestNames_PlatformManifestsChangeForcesFullSuiteEvenWithAResolvedComponent(t *testing.T) {
	root, _ := initGitRepo(t)
	configPath := filepath.Join(root, "manifests-config.yaml")

	require.NoError(t, os.WriteFile(configPath, []byte(`
components:
  kserve:
    odh:
      repo: opendatahub-io/example
      ref: main@aaa
      sourcePath: config
platformManifests:
  rhoai: config
`), 0o600))
	runGitForTest(t, root, "add", "manifests-config.yaml")
	runGitForTest(t, root, "commit", "-q", "-m", "config")
	configCommit := strings.TrimSpace(runGitForTest(t, root, "rev-parse", "HEAD"))

	require.NoError(t, os.WriteFile(configPath, []byte(`
components:
  kserve:
    odh:
      repo: opendatahub-io/example
      ref: main@bbb
      sourcePath: config
platformManifests:
  rhoai: config-v2
`), 0o600))

	_, err := ResolveManifestNames(context.Background(), root, configPath, configCommit, nil)
	assert.Error(t, err, "a platformManifests change must force the full suite even though kserve also resolved")
}

func TestLoadManifestsConfigAtRef_IllegalImageOverrideKeyStillParses(t *testing.T) {
	root, _ := initGitRepo(t)
	configPath := filepath.Join(root, "manifests-config.yaml")

	require.NoError(t, os.WriteFile(configPath, []byte(illegalImageOverrideKeyYAML), 0o600))
	runGitForTest(t, root, "add", "manifests-config.yaml")
	runGitForTest(t, root, "commit", "-q", "-m", "illegal key")
	commit := strings.TrimSpace(runGitForTest(t, root, "rev-parse", "HEAD"))

	cfg, err := loadManifestsConfigAtRef(context.Background(), commit, configPath)
	require.NoError(t, err)
	_, ok := cfg.ImageOverrides["NOT_A_RELATED_IMAGE"]
	assert.True(t, ok, "git show of a snapshot with an illegal imageOverrides key must still unmarshal")
}
