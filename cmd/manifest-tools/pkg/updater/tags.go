package updater

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

type TagsOptions struct {
	ConfigFile string
	TrackerURL string
	GH         GitHubClient
}

type TagsResult struct {
	Updated      int
	ImageEnvVars map[string]string // RELATED_IMAGE_ODH_*_IMAGE → image reference
}

var (
	releaseLineRegex = regexp.MustCompile(`\s*[A-Za-z\-_0-9/]+\s*\|\s*(https://github\.com/.*(tree|releases).*)\s*\|?\s*(https://github\.com/.*releases.*)?\s*`)
	imageNameRegex   = regexp.MustCompile(`^[A-Za-z0-9\-_]+$`)
	imageRefRegex    = regexp.MustCompile(`^[a-z0-9.\-]+(?::[0-9]+)?/[a-zA-Z0-9_.\-/]+(?::[a-zA-Z0-9_.\-]+|@[a-z0-9]+:[a-f0-9]+)$`)
)

func UpdateTags(ctx context.Context, opts TagsOptions) (*TagsResult, error) {
	if opts.TrackerURL == "" {
		return nil, fmt.Errorf("tracker URL is required")
	}

	owner, repo, issueNumber, err := parseTrackerURL(opts.TrackerURL)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return nil, err
	}

	nodeDoc, err := config.LoadNode(opts.ConfigFile)
	if err != nil {
		return nil, err
	}

	gh := opts.GH

	comments, err := gh.GetIssueComments(ctx, owner, repo, issueNumber)
	if err != nil {
		return nil, fmt.Errorf("fetching tracker issue comments: %w", err)
	}

	// Parse all comments for #Release# and #Images# sections
	type componentUpdate struct {
		name   string
		branch string
		sha    string
	}

	var componentUpdates []componentUpdate
	imageEnvVars := make(map[string]string)

	for _, comment := range comments {
		lines := strings.Split(comment.Body, "\n")

		releaseIdx := indexOf(lines, "#Release#")
		if releaseIdx >= 0 {
			for _, line := range lines[releaseIdx+1:] {
				if !releaseLineRegex.MatchString(line) {
					continue
				}

				parts := strings.SplitN(line, "|", 3)
				if len(parts) < 2 {
					continue
				}

				componentName := strings.TrimSpace(parts[0])
				branchURL := strings.TrimSpace(parts[1])

				splitArr := strings.Split(branchURL, "/")
				if len(splitArr) < 5 {
					slog.Warn("Skipping malformed URL", slog.String("component", componentName), slog.String("url", branchURL))
					continue
				}

				idx := -1
				for i, s := range splitArr {
					if s == "tag" || s == "tree" {
						idx = i
						break
					}
				}
				if idx < 0 || idx+1 >= len(splitArr) {
					slog.Warn("No tag/tree segment in URL", slog.String("component", componentName), slog.String("url", branchURL))
					continue
				}

				branchName := strings.Join(splitArr[idx+1:], "/")
				repoOrg := splitArr[3]
				repoName := splitArr[4]

				slog.Info("Processing component", slog.String("name", componentName))

				commitSHA, err := gh.GetLatestCommitSHA(ctx, repoOrg, repoName, branchName)
				if err != nil {
					return nil, fmt.Errorf("resolving SHA for %s (%s/%s ref %s): %w", componentName, repoOrg, repoName, branchName, err)
				}

				if componentName == "workbenches/notebook-controller" {
					componentUpdates = append(componentUpdates,
						componentUpdate{name: "odh-notebook-controller", branch: branchName, sha: commitSHA},
						componentUpdate{name: "kf-notebook-controller", branch: branchName, sha: commitSHA},
					)
				} else {
					normalizedName := strings.ToLower(strings.ReplaceAll(componentName, "/", "-"))
					componentUpdates = append(componentUpdates, componentUpdate{name: normalizedName, branch: branchName, sha: commitSHA})
				}
			}
		}

		imagesIdx := indexOf(lines, "#Images#")
		if imagesIdx >= 0 {
			slog.Info("Found #Images# section in tracker comment")
			for _, line := range lines[imagesIdx+1:] {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" {
					continue
				}
				trimmed = strings.TrimPrefix(trimmed, "- ")
				trimmed = strings.TrimSpace(trimmed)

				imgParts := strings.SplitN(trimmed, "|", 2)
				if len(imgParts) != 2 {
					continue
				}

				imageName := strings.TrimSpace(imgParts[0])
				imageRef := strings.TrimSpace(imgParts[1])

				if !imageNameRegex.MatchString(imageName) || !imageRefRegex.MatchString(imageRef) {
					continue
				}

				envVar := imageNameToEnvVar(imageName)
				imageEnvVars[envVar] = imageRef
				slog.Info("Operator image", slog.String("env", envVar), slog.String("ref", imageRef))
			}
		}
	}

	// Apply component updates to ODH entries in manifests-config.yaml
	odhComponents := collectODHComponents(cfg)
	var updated int

	for _, cu := range componentUpdates {
		matched := false
		for _, mc := range odhComponents {
			normalizedManifest := normalizeName(mc.componentName)
			normalizedManifestNoPrefix := normalizeName(strings.TrimPrefix(mc.componentName, "workbenches/"))
			normalizedKey := normalizeName(cu.name)

			if normalizedManifest == normalizedKey || normalizedManifestNoPrefix == normalizedKey {
				newRef := cu.branch
				if cu.sha != "" {
					newRef = fmt.Sprintf("%s@%s", cu.branch, cu.sha)
				}

				slog.Info("Updating", slog.String("component", mc.componentName), slog.String("ref", newRef))
				if err := nodeDoc.SetComponentRef(mc.section, mc.componentName, "odh", newRef); err != nil {
					slog.Warn("Failed to set ref", slog.String("error", err.Error()))
					continue
				}
				updated++
				matched = true
				break
			}
		}
		if !matched {
			slog.Warn("No matching component found", slog.String("key", cu.name))
		}
	}

	if updated > 0 {
		if err := nodeDoc.Save(opts.ConfigFile); err != nil {
			return nil, fmt.Errorf("saving config: %w", err)
		}
	}

	slog.Info("Tags update complete", slog.Int("components-updated", updated), slog.Int("images-found", len(imageEnvVars)))

	return &TagsResult{
		Updated:      updated,
		ImageEnvVars: imageEnvVars,
	}, nil
}

func parseTrackerURL(url string) (owner, repo string, issueNumber int, err error) {
	parts := strings.Split(url, "/")
	if len(parts) < 7 {
		return "", "", 0, fmt.Errorf("invalid tracker URL: %s", url)
	}
	owner = parts[3]
	repo = parts[4]
	issueNumber, err = strconv.Atoi(parts[6])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid issue number in URL %s: %w", url, err)
	}
	return owner, repo, issueNumber, nil
}

func imageNameToEnvVar(imageName string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(imageName, "-", "_"))
	if strings.HasSuffix(normalized, "_IMAGE") {
		return "RELATED_IMAGE_ODH_" + normalized
	}
	return "RELATED_IMAGE_ODH_" + normalized + "_IMAGE"
}

type manifestComponent struct {
	componentName string
	section       string
}

func collectODHComponents(cfg *config.ManifestsConfig) []manifestComponent {
	var result []manifestComponent

	for name, comp := range cfg.Components {
		if comp.ODH != nil {
			result = append(result, manifestComponent{componentName: name, section: "components"})
		}
	}
	for name, comp := range cfg.CCMCharts {
		if comp.ODH != nil {
			result = append(result, manifestComponent{componentName: name, section: "ccmCharts"})
		}
	}
	for name, comp := range cfg.ComponentCharts {
		if comp.ODH != nil {
			result = append(result, manifestComponent{componentName: name, section: "componentCharts"})
		}
	}

	return result
}

func normalizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("/", "-", "_", "-").Replace(s)
	return s
}

func indexOf(lines []string, target string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			return i
		}
	}
	return -1
}
