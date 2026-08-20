package updater

import (
	"context"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/github"
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
