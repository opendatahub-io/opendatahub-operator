package updater

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

type RHOAIBranchOptions struct {
	ConfigFile string
	NewBranch  string
	GH         GitHubClient
}

func UpdateRHOAIBranch(ctx context.Context, opts RHOAIBranchOptions) (bool, error) {
	if opts.NewBranch == "" {
		return false, fmt.Errorf("new branch name is required")
	}

	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return false, err
	}

	nodeDoc, err := config.LoadNode(opts.ConfigFile)
	if err != nil {
		return false, err
	}

	gh := opts.GH

	sections := allSections(cfg)

	var updated int
	var missingBranches []string

	for _, sec := range sections {
		for compName, comp := range sec.components {
			pr := comp.PlatformRepo("rhoai")
			if pr == nil || pr.Ref == "" {
				continue
			}

			if config.ExtractSHA(pr.Ref) == "" {
				continue
			}

			orgRepo := strings.SplitN(pr.Repo, "/", 2)
			if len(orgRepo) != 2 {
				continue
			}

			slog.Info("Updating", slog.String("component", compName), slog.String("repo", pr.Repo), slog.String("branch", opts.NewBranch))

			latestSHA, err := gh.GetLatestCommitSHA(ctx, orgRepo[0], orgRepo[1], opts.NewBranch)
			if err != nil {
				missingBranches = append(missingBranches, pr.Repo)
				slog.Warn("Branch not found", slog.String("repo", pr.Repo), slog.String("branch", opts.NewBranch))
				continue
			}

			newRef := fmt.Sprintf("%s@%s", opts.NewBranch, latestSHA)
			if err := nodeDoc.SetComponentRef(sec.name, compName, "rhoai", newRef); err != nil {
				slog.Warn("Failed to set ref", slog.String("error", err.Error()))
				continue
			}
			updated++
		}
	}

	if len(missingBranches) > 0 {
		return false, fmt.Errorf("branch %q not found in: %s", opts.NewBranch, strings.Join(missingBranches, ", "))
	}

	if updated == 0 {
		slog.Info("No RHOAI manifest updates needed")
		return false, nil
	}

	if err := nodeDoc.Save(opts.ConfigFile); err != nil {
		return false, fmt.Errorf("saving config: %w", err)
	}

	slog.Info("RHOAI manifests updated", slog.Int("count", updated), slog.String("branch", opts.NewBranch))
	return true, nil
}
