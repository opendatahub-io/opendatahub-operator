package updater

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/github"
)

var (
	branchNameRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
	commitSHARegex  = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type GitHubClient interface {
	GetLatestCommitSHA(ctx context.Context, owner, repo, ref string) (string, error)
	GetIssueComments(ctx context.Context, owner, repo string, issueNumber int) ([]github.IssueComment, error)
}

type sectionEntry struct {
	name       string
	components map[string]config.Component
}

func allSections(cfg *config.ManifestsConfig) []sectionEntry {
	return []sectionEntry{
		{"components", cfg.Components},
		{"ccmCharts", cfg.CCMCharts},
		{"componentCharts", cfg.ComponentCharts},
	}
}

func validateBranchName(branch string) error {
	if !branchNameRegex.MatchString(branch) || strings.Contains(branch, "..") {
		return fmt.Errorf("invalid branch name: %q", branch)
	}
	return nil
}

func validCommitSHA(sha string) bool {
	return commitSHARegex.MatchString(sha)
}
