package updater

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

type SHAsOptions struct {
	ConfigFile string
	GH         GitHubClient
}

type SHAsResult struct {
	Updated          bool
	FailedComponents []string
}

func UpdateSHAs(ctx context.Context, opts SHAsOptions) (SHAsResult, error) {
	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return SHAsResult{}, err
	}

	nodeDoc, err := config.LoadNode(opts.ConfigFile)
	if err != nil {
		return SHAsResult{}, err
	}

	gh := opts.GH

	sections := allSections(cfg)

	var updated int
	var successfulFetches int
	failedSet := map[string]struct{}{}

	for _, sec := range sections {
		for compName, comp := range sec.components {
			for _, platform := range []string{"odh", "rhoai"} {
				pr := comp.PlatformRepo(platform)
				if pr == nil || pr.Ref == "" {
					continue
				}

				currentSHA := config.ExtractSHA(pr.Ref)
				branch := config.ExtractBranch(pr.Ref)
				if currentSHA == "" {
					continue
				}

				orgRepo := strings.SplitN(pr.Repo, "/", 2)
				if len(orgRepo) != 2 {
					continue
				}

				slog.Info("Checking", slog.String("platform", platform), slog.String("component", compName), slog.String("branch", branch))

				latestSHA, err := gh.GetLatestCommitSHA(ctx, orgRepo[0], orgRepo[1], branch)
				if err != nil {
					slog.Warn("Failed to fetch SHA", slog.String("component", compName), slog.String("error", err.Error()))
					failedSet[compName] = struct{}{}
					continue
				}
				successfulFetches++

				if latestSHA == currentSHA {
					continue
				}

				newRef := fmt.Sprintf("%s@%s", branch, latestSHA)
				slog.Info("Update needed",
					slog.String("platform", platform),
					slog.String("component", compName),
					slog.String("old", currentSHA[:min(8, len(currentSHA))]),
					slog.String("new", latestSHA[:min(8, len(latestSHA))]))

				if err := nodeDoc.SetComponentRef(sec.name, compName, platform, newRef); err != nil {
					slog.Warn("Failed to set ref", slog.String("error", err.Error()))
					continue
				}
				updated++
			}
		}
	}

	failedComponents := make([]string, 0, len(failedSet))
	for c := range failedSet {
		failedComponents = append(failedComponents, c)
	}
	sort.Strings(failedComponents)
	result := SHAsResult{Updated: updated > 0, FailedComponents: failedComponents}

	if successfulFetches == 0 && len(failedComponents) > 0 {
		return result, fmt.Errorf("%d component SHA fetch(es) failed and no SHAs were updated", len(failedComponents))
	}

	if updated == 0 {
		slog.Info("All manifest references are up to date")
		return result, nil
	}

	if err := nodeDoc.Save(opts.ConfigFile); err != nil {
		return result, fmt.Errorf("saving config: %w", err)
	}

	slog.Info("Manifest SHAs updated", slog.Int("count", updated))
	return result, nil
}
