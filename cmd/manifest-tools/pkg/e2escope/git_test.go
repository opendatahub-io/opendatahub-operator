// This is an internal test (package e2escope, not e2escope_test) because
// gitDiffBase and changedFiles are unexported -- this is this package's
// only coverage of the real (non-overridden) git path, matching
// attribution_test.go's own precedent in this same package.
//
//nolint:testpackage // deliberate internal test, see comment above
package e2escope

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitDiffBase_RejectsMalformedPullBaseSHA(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"leading dash reads as a git option, not a revision", "--upload-pack=x"},
		{"empty after trimming is never set, but a lone dash still isn't a SHA", "-"},
		{"not hexadecimal", "not-a-sha"},
		{"too short to be an abbreviated SHA", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PULL_BASE_SHA", tt.value)
			_, err := gitDiffBase(context.Background(), t.TempDir())
			assert.Error(t, err, "a PULL_BASE_SHA that isn't a plain hex SHA must be rejected before it reaches a git argument")
		})
	}
}

func TestGitDiffBase_AcceptsAValidPullBaseSHA(t *testing.T) {
	t.Setenv("PULL_BASE_SHA", "0123456789abcdef0123456789abcdef01234567")
	base, err := gitDiffBase(context.Background(), t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", base)
}

// runGitForTest runs git in dir with a fixed author/committer identity, so
// tests don't depend on the environment's own git config.
func runGitForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// initGitRepo creates a real repository with two commits, so changedFiles
// can be exercised against actual git output instead of a stub -- this
// package's only test coverage of the real (non-overridden) git path.
// Returns the repo root and the first commit's SHA.
func initGitRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()

	runGitForTest(t, root, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600))
	runGitForTest(t, root, "add", "a.txt")
	runGitForTest(t, root, "commit", "-q", "-m", "first")
	first := strings.TrimSpace(runGitForTest(t, root, "rev-parse", "HEAD"))

	require.NoError(t, os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o600))
	runGitForTest(t, root, "add", "b.txt")
	runGitForTest(t, root, "commit", "-q", "-m", "second")

	return root, first
}

func TestChangedFiles_ListsFilesBetweenBaseAndHead(t *testing.T) {
	root, first := initGitRepo(t)

	files, err := changedFiles(context.Background(), root, first)
	require.NoError(t, err)
	assert.Equal(t, []string{"b.txt"}, files)
}

func TestChangedFiles_BaseEqualToHeadHasNoDiff(t *testing.T) {
	root, _ := initGitRepo(t)
	head := strings.TrimSpace(runGitForTest(t, root, "rev-parse", "HEAD"))

	_, err := changedFiles(context.Background(), root, head)
	assert.ErrorIs(t, err, errNoDiff)
}
