package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/github"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/updater"
)

func newUpdateRefsCommand(root *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-refs",
		Short: "Update component ref fields in manifests-config.yaml",
	}

	cmd.AddCommand(
		newUpdateRefsSHAsCommand(root),
		newUpdateRefsTagsCommand(root),
		newUpdateRefsRHOAIBranchCommand(root),
	)

	return cmd
}

func newUpdateRefsSHAsCommand(root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "shas",
		Short: "Update branch@sha refs to latest commit SHAs from GitHub",
		RunE: func(cmd *cobra.Command, args []string) error {
			gh, err := github.NewClient()
			if err != nil {
				return err
			}

			result, err := updater.UpdateSHAs(cmd.Context(), updater.SHAsOptions{
				ConfigFile: root.configFile,
				GH:         gh,
			})

			writeGitHubOutput("updates-needed", fmt.Sprintf("%t", result.Updated))
			if len(result.FailedComponents) > 0 {
				writeGitHubOutput("fetch-failed", "true")
				writeGitHubOutput("failed-components", strings.Join(result.FailedComponents, ", "))
			}

			if err != nil {
				return err
			}
			return nil
		},
	}
}

func newUpdateRefsTagsCommand(root *rootOptions) *cobra.Command {
	var trackerURL string

	cmd := &cobra.Command{
		Use:   "tags",
		Short: "Parse tracker issue and update ODH component refs and operator images",
		RunE: func(cmd *cobra.Command, args []string) error {
			if trackerURL == "" {
				trackerURL = os.Getenv("TRACKER_URL")
			}

			gh, err := github.NewClient()
			if err != nil {
				return err
			}

			result, err := updater.UpdateTags(cmd.Context(), updater.TagsOptions{
				ConfigFile: root.configFile,
				TrackerURL: trackerURL,
				GH:         gh,
			})
			if err != nil {
				return err
			}

			for envName, imageRef := range result.ImageEnvVars {
				writeGitHubOutput(envName, imageRef)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&trackerURL, "tracker-url", "", "GitHub tracker issue URL (or TRACKER_URL env)")

	return cmd
}

func newUpdateRefsRHOAIBranchCommand(root *rootOptions) *cobra.Command {
	var newBranch string

	cmd := &cobra.Command{
		Use:   "rhoai-branch",
		Short: "Update all RHOAI component refs to a new branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			if newBranch == "" {
				newBranch = os.Getenv("NEW_RHOAI_BRANCH")
			}

			gh, err := github.NewClient()
			if err != nil {
				return err
			}

			updated, err := updater.UpdateRHOAIBranch(cmd.Context(), updater.RHOAIBranchOptions{
				ConfigFile: root.configFile,
				NewBranch:  newBranch,
				GH:         gh,
			})
			if err != nil {
				return err
			}

			writeGitHubOutput("updates-needed", fmt.Sprintf("%t", updated))
			return nil
		},
	}

	cmd.Flags().StringVar(&newBranch, "branch", "", "New RHOAI branch name (or NEW_RHOAI_BRANCH env)")

	return cmd
}

func writeGitHubOutput(key, value string) {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		return
	}

	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		slog.Warn("Failed to open GITHUB_OUTPUT", slog.String("error", err.Error()))
		return
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s=%s\n", key, value); err != nil {
		slog.Warn("Failed to write GITHUB_OUTPUT", slog.String("key", key), slog.String("error", err.Error()))
	}
}
