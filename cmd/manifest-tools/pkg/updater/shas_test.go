package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/github"
)

type mockGitHub struct {
	shas     map[string]string // "owner/repo/ref" → sha
	comments []github.IssueComment
	err      error
}

func (m *mockGitHub) GetLatestCommitSHA(_ context.Context, owner, repo, ref string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	key := owner + "/" + repo + "/" + ref
	if sha, ok := m.shas[key]; ok {
		return sha, nil
	}
	return "", fmt.Errorf("ref %s not found", ref)
}

func (m *mockGitHub) GetIssueComments(_ context.Context, _, _ string, _ int) ([]github.IssueComment, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.comments, nil
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifests-config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const testConfigWithSHA = `components:
  dashboard:
    odh:
      repo: "opendatahub-io/dashboard"
      ref: "main@aaa1111222233334444555566667777aaaabbbb"
      sourcePath: "config"
  ray:
    odh:
      repo: "opendatahub-io/kuberay"
      ref: "dev@1111222233334444555566667777aaaabbbbcccc"
      sourcePath: "config"
ccmCharts: {}
componentCharts: {}
`

func TestUpdateSHAs_UpdatesChanged(t *testing.T) {
	configPath := writeTestConfig(t, testConfigWithSHA)

	gh := &mockGitHub{
		shas: map[string]string{
			"opendatahub-io/dashboard/main": "newsha111222233334444555566667777aaaabbbb",
			"opendatahub-io/kuberay/dev":    "1111222233334444555566667777aaaabbbbcccc",
		},
	}

	result, err := UpdateSHAs(context.Background(), SHAsOptions{
		ConfigFile: configPath,
		GH:         gh,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Updated {
		t.Fatal("expected updates-needed=true")
	}
	if len(result.FailedComponents) > 0 {
		t.Fatalf("expected no failed components, got %v", result.FailedComponents)
	}
}

func TestUpdateSHAs_NoChanges(t *testing.T) {
	configPath := writeTestConfig(t, testConfigWithSHA)

	gh := &mockGitHub{
		shas: map[string]string{
			"opendatahub-io/dashboard/main": "aaa1111222233334444555566667777aaaabbbb",
			"opendatahub-io/kuberay/dev":    "1111222233334444555566667777aaaabbbbcccc",
		},
	}

	result, err := UpdateSHAs(context.Background(), SHAsOptions{
		ConfigFile: configPath,
		GH:         gh,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Updated {
		t.Fatal("expected updates-needed=false when SHAs match")
	}
	if len(result.FailedComponents) > 0 {
		t.Fatalf("expected no failed components, got %v", result.FailedComponents)
	}
}

func TestUpdateSHAs_SkipsComponentsWithoutSHA(t *testing.T) {
	cfg := `components:
  dashboard:
    odh:
      repo: "opendatahub-io/dashboard"
      ref: "main"
      sourcePath: "config"
ccmCharts: {}
componentCharts: {}
`
	configPath := writeTestConfig(t, cfg)

	gh := &mockGitHub{shas: map[string]string{}}

	result, err := UpdateSHAs(context.Background(), SHAsOptions{
		ConfigFile: configPath,
		GH:         gh,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Updated {
		t.Fatal("expected no updates for refs without SHA")
	}
	if len(result.FailedComponents) > 0 {
		t.Fatalf("expected no failed components, got %v", result.FailedComponents)
	}
}

func TestUpdateSHAs_InvalidConfigPath(t *testing.T) {
	gh := &mockGitHub{}
	_, err := UpdateSHAs(context.Background(), SHAsOptions{
		ConfigFile: "/nonexistent/path.yaml",
		GH:         gh,
	})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestUpdateSHAs_PartialFailure_StillUpdatesOthers(t *testing.T) {
	configPath := writeTestConfig(t, testConfigWithSHA)

	// dashboard fetch fails, ray fetch succeeds with new SHA
	gh := &mockGitHub{
		shas: map[string]string{
			"opendatahub-io/kuberay/dev": "newsha222233334444555566667777aaaabbbbcccc",
		},
		err: nil, // Will fail for dashboard (not in map), succeed for ray
	}

	result, err := UpdateSHAs(context.Background(), SHAsOptions{
		ConfigFile: configPath,
		GH:         gh,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Updated {
		t.Fatal("expected updates-needed=true (ray was updated)")
	}
	if len(result.FailedComponents) != 1 || result.FailedComponents[0] != "dashboard" {
		t.Fatalf("expected failed-components=[dashboard], got %v", result.FailedComponents)
	}
}

func TestUpdateSHAs_AllFetchesFail_ReturnsError(t *testing.T) {
	configPath := writeTestConfig(t, testConfigWithSHA)

	// All fetches fail (network error)
	gh := &mockGitHub{
		err: fmt.Errorf("network error"),
	}

	result, err := UpdateSHAs(context.Background(), SHAsOptions{
		ConfigFile: configPath,
		GH:         gh,
	})
	if err == nil {
		t.Fatal("expected error when all fetches fail")
	}
	if result.Updated {
		t.Fatal("expected updated=false when all fail")
	}
	if len(result.FailedComponents) != 2 {
		t.Fatalf("expected 2 failed components, got %d", len(result.FailedComponents))
	}
}
