package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/flakerate"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/jira"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/quarantine"
)

// NewFlakeReportCommand creates the flake-report subcommand.
func NewFlakeReportCommand(cfg *config.Config) *cobra.Command {
	var (
		junitDir         string
		threshold        float64
		windowDays       int
		quarantineCfgOut string
		autoQuarantine   bool
		outputJSON       bool
		outputMarkdown   bool
		jiraOpts         jira.Options
		jiraConfigPath   string
	)

	cmd := &cobra.Command{
		Use:   "flake-report",
		Short: "Analyze JUnit XML artifacts and compute per-test flake rates",
		Long: `Reads JUnit XML files from a directory, aggregates per-test pass/fail
results, computes flake rates, classifies failures as flaky or regression,
and optionally generates a quarantine config for flaky tests exceeding the
threshold. Regressions are never auto-quarantined.

Example:
  test-retry flake-report --junit-dir ./artifacts/ --threshold 0.2
  test-retry flake-report --junit-dir ./artifacts/ --threshold 0.2 --auto-quarantine --quarantine-config quarantine.json`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputJSON && outputMarkdown {
				return fmt.Errorf("--json and --markdown are mutually exclusive")
			}
			if threshold < 0 || threshold > 1 {
				return fmt.Errorf("--threshold must be between 0.0 and 1.0, got %.2f", threshold)
			}
			if windowDays <= 0 {
				return fmt.Errorf("--window-days must be > 0, got %d", windowDays)
			}

			// Load Jira config from file first, then let flags/env override.
			if jiraConfigPath != "" {
				fileCfg, err := jira.LoadOptionsFromFile(jiraConfigPath)
				if err != nil {
					return fmt.Errorf("loading jira config: %w", err)
				}
				if jiraOpts.Server == "" {
					jiraOpts.Server = fileCfg.Server
				}
				if jiraOpts.Token == "" {
					jiraOpts.Token = fileCfg.Token
				}
				if jiraOpts.Project == "" {
					jiraOpts.Project = fileCfg.Project
				}
				if len(jiraOpts.Labels) == 0 {
					jiraOpts.Labels = fileCfg.Labels
				}
			}

			if jiraOpts.Token == "" {
				jiraOpts.Token = viper.GetString("jira-token")
			}

			report, err := flakerate.AnalyzeDir(junitDir)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			if report.TotalFiles == 0 {
				if outputJSON {
					return printJSONReport(report, threshold)
				}
				if outputMarkdown {
					fmt.Println("No JUnit XML files found — nothing to report.")
					return nil
				}
				fmt.Println("No JUnit XML files found in", junitDir)
				return nil
			}

			switch {
			case outputJSON:
				if err := printJSONReport(report, threshold); err != nil {
					return err
				}
			case outputMarkdown:
				var qcfg *quarantine.Config
				if quarantineCfgOut != "" {
					qcfg, err = quarantine.LoadConfig(quarantineCfgOut)
					if err != nil {
						return fmt.Errorf("failed to load quarantine config for markdown report: %w", err)
					}
				}
				printMarkdownReport(report, threshold, qcfg)
			default:
				printTextReport(report, threshold, cfg.Verbose)
			}

			if autoQuarantine {
				if quarantineCfgOut == "" {
					return fmt.Errorf("--auto-quarantine requires --quarantine-config")
				}
				return writeQuarantineConfig(report, threshold, windowDays, quarantineCfgOut, outputJSON, &jiraOpts)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&junitDir, "junit-dir", "", "Directory containing JUnit XML files (required)")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.2, "Flake rate threshold (0.0-1.0) above which tests are flagged")
	cmd.Flags().IntVar(&windowDays, "window-days", 30, "Rolling window in days (used in quarantine metadata)")
	cmd.Flags().StringVar(&quarantineCfgOut, "quarantine-config", "", "Path to write quarantine config JSON")
	cmd.Flags().BoolVar(&autoQuarantine, "auto-quarantine", false, "Automatically generate quarantine entries for flaky tests exceeding threshold")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output report as JSON")
	cmd.Flags().BoolVar(&outputMarkdown, "markdown", false, "Output report as GitHub-flavored markdown")

	// Jira integration flags (optional, used with --auto-quarantine)
	cmd.Flags().StringVar(&jiraConfigPath, "jira-config", "", "Path to JSON file with Jira config (server, token, project, labels)")
	cmd.Flags().StringVar(&jiraOpts.Server, "jira-server", "", "Jira server URL for creating quarantine tickets (e.g. https://redhat.atlassian.net)")
	cmd.Flags().StringVar(&jiraOpts.Token, "jira-token", "", "Jira API token (can also use JIRA_TOKEN env var)")
	cmd.Flags().StringVar(&jiraOpts.Project, "jira-project", "", "Jira project key for quarantine tickets (e.g. RHOAIENG)")

	viper.BindPFlag("jira-token", cmd.Flags().Lookup("jira-token"))        //nolint:errcheck
	viper.BindEnv("jira-token", "QUARANTINE_JIRA_API_TOKEN", "JIRA_TOKEN") //nolint:errcheck

	_ = cmd.MarkFlagRequired("junit-dir")

	return cmd
}

type jsonReport struct {
	TotalFiles  int              `json:"total_files"`
	TotalTests  int              `json:"total_tests"`
	Threshold   float64          `json:"threshold"`
	Regressions []jsonTestRecord `json:"regressions"`
	Flaky       []jsonTestRecord `json:"flaky_tests"`
}

type jsonTestRecord struct {
	Name             string  `json:"name"`
	Pattern          string  `json:"pattern"`
	FlakeRate        float64 `json:"flake_rate"`
	TotalRuns        int     `json:"total_runs"`
	FailedRuns       int     `json:"failed_runs"`
	PassedRuns       int     `json:"passed_runs"`
	TransitionCommit string  `json:"transition_commit,omitempty"`
}

func toJSONRecord(r *flakerate.TestRecord) jsonTestRecord {
	return jsonTestRecord{
		Name:             r.Name,
		Pattern:          string(r.ClassifyPattern()),
		FlakeRate:        r.FlakeRate(),
		TotalRuns:        r.TotalRuns,
		FailedRuns:       r.FailedRuns,
		PassedRuns:       r.PassedRuns,
		TransitionCommit: r.TransitionCommit(),
	}
}

func printJSONReport(report *flakerate.Report, threshold float64) error {
	regressions := report.Regressions()
	flaky := report.FlakyTests(threshold)

	jr := jsonReport{
		TotalFiles:  report.TotalFiles,
		TotalTests:  len(report.Tests),
		Threshold:   threshold,
		Regressions: make([]jsonTestRecord, 0, len(regressions)),
		Flaky:       make([]jsonTestRecord, 0, len(flaky)),
	}

	for _, r := range regressions {
		jr.Regressions = append(jr.Regressions, toJSONRecord(r))
	}
	for _, r := range flaky {
		jr.Flaky = append(jr.Flaky, toJSONRecord(r))
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(jr)
}

func printTextReport(report *flakerate.Report, threshold float64, verbose bool) {
	fmt.Printf("Flake Rate Report\n")
	fmt.Printf("─────────────────\n")
	fmt.Printf("Files analyzed: %d\n", report.TotalFiles)
	fmt.Printf("Unique tests:   %d\n", len(report.Tests))
	fmt.Printf("Threshold:      %.0f%%\n\n", threshold*100)

	regressions := report.Regressions()
	if len(regressions) > 0 {
		fmt.Printf("Regressions (%d) — consistent failures after a transition:\n", len(regressions))
		for _, r := range regressions {
			commit := r.TransitionCommit()
			if commit != "" {
				if len(commit) > 8 {
					commit = commit[:8]
				}
				fmt.Printf("  ✗ %-65s  %5.1f%%  (%d/%d)  commit=%s\n",
					r.Name, r.FlakeRate()*100, r.FailedRuns, r.TotalRuns, commit)
			} else {
				fmt.Printf("  ✗ %-65s  %5.1f%%  (%d/%d)\n",
					r.Name, r.FlakeRate()*100, r.FailedRuns, r.TotalRuns)
			}
		}
		fmt.Println()
	}

	flaky := report.FlakyTests(threshold)
	if len(flaky) > 0 {
		fmt.Printf("Flaky tests exceeding %.0f%% (%d) — intermittent, safe to quarantine:\n", threshold*100, len(flaky))
		for _, r := range flaky {
			fmt.Printf("  ~ %-65s  %5.1f%%  (%d/%d failed)\n",
				r.Name, r.FlakeRate()*100, r.FailedRuns, r.TotalRuns)
		}
		fmt.Println()
	}

	if len(regressions) == 0 && len(flaky) == 0 {
		fmt.Println("No regressions or flaky tests detected.")
	}

	if verbose {
		fmt.Printf("All tests:\n")
		all := sortedRecords(report)
		for _, r := range all {
			pattern := r.ClassifyPattern()
			marker := " "
			switch pattern {
			case flakerate.PatternRegression:
				marker = "✗"
			case flakerate.PatternFlaky:
				marker = "~"
			case flakerate.PatternPersistent:
				marker = "!"
			}
			fmt.Printf("  %s %-65s  %5.1f%%  (%d/%d)  [%s]\n",
				marker, r.Name, r.FlakeRate()*100, r.FailedRuns, r.TotalRuns, pattern)
		}
	}
}

func printMarkdownReport(report *flakerate.Report, threshold float64, qcfg *quarantine.Config) {
	regressions := report.Regressions()
	flaky := report.FlakyTests(threshold)

	fmt.Printf("## Flake Report — %s\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Printf("**Files analyzed:** %d | **Tests tracked:** %d | **Threshold:** %.0f%%\n\n",
		report.TotalFiles, len(report.Tests), threshold*100)

	if len(regressions) > 0 {
		fmt.Printf("### Regressions (%d)\n\n", len(regressions))
		fmt.Println("| Test | Flake Rate | Failed/Total | Transition Commit |")
		fmt.Println("|------|-----------|--------------|-------------------|")
		for _, r := range regressions {
			commit := r.TransitionCommit()
			if commit == "" {
				commit = "\u2014"
			}
			fmt.Printf("| `%s` | %.0f%% | %d/%d | %s |\n",
				r.Name, r.FlakeRate()*100, r.FailedRuns, r.TotalRuns, commit)
		}
		fmt.Println()
	}

	if len(flaky) > 0 {
		fmt.Printf("### Flaky Tests (%d)\n\n", len(flaky))
		fmt.Println("| Test | Flake Rate | Failed/Total |")
		fmt.Println("|------|-----------|--------------|")
		for _, r := range flaky {
			fmt.Printf("| `%s` | %.0f%% | %d/%d |\n",
				r.Name, r.FlakeRate()*100, r.FailedRuns, r.TotalRuns)
		}
		fmt.Println()
	}

	if qcfg != nil && len(qcfg.Tests) > 0 {
		fmt.Printf("### Quarantined Tests (%d)\n\n", len(qcfg.Tests))
		fmt.Println("| Test | Jira | Flake Rate | Re-enable After |")
		fmt.Println("|------|------|-----------|-----------------|")
		names := qcfg.QuarantinedNames()
		for _, name := range names {
			e := qcfg.Tests[name]
			jiraRef := "\u2014"
			if e.Jira != "" {
				jiraRef = e.Jira
			}
			reEnable := "never"
			if e.ReEnableAfter != "" {
				reEnable = e.ReEnableAfter
			}
			fmt.Printf("| `%s` | %s | %.0f%% | %s |\n",
				name, jiraRef, e.FlakeRate*100, reEnable)
		}
		fmt.Println()
	}

	if len(regressions) == 0 && len(flaky) == 0 {
		fmt.Println("All tests healthy — no regressions or flaky tests detected.")
		fmt.Println()
	}
}

func sortedRecords(report *flakerate.Report) []*flakerate.TestRecord {
	records := make([]*flakerate.TestRecord, 0, len(report.Tests))
	for _, r := range report.Tests {
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].FlakeRate() != records[j].FlakeRate() {
			return records[i].FlakeRate() > records[j].FlakeRate()
		}
		return records[i].Name < records[j].Name
	})
	return records
}

func writeQuarantineConfig(report *flakerate.Report, threshold float64, windowDays int, outPath string, jsonMode bool, jiraOpts *jira.Options) error {
	w := os.Stdout
	if jsonMode {
		w = os.Stderr
	}

	cfg, err := quarantine.LoadConfig(outPath)
	if err != nil {
		return fmt.Errorf("failed to load existing quarantine config: %w", err)
	}

	removed := cfg.RemoveExpired()
	if removed > 0 {
		fmt.Fprintf(w, "  🗑 removed %d expired quarantine entries\n", removed)
	}

	if jiraOpts.Configured() {
		removed += unquarantineResolved(cfg, jiraOpts, w)
	}

	newEntries := report.AutoQuarantine(threshold, windowDays)
	added := 0
	updated := 0
	var newlyQuarantined []quarantine.Entry

	for _, entry := range newEntries {
		alreadyQuarantined, _ := cfg.IsQuarantined(entry.Name)
		cfg.AddOrUpdate(entry)
		if !alreadyQuarantined {
			added++
			newlyQuarantined = append(newlyQuarantined, entry)
			fmt.Fprintf(w, "  + quarantined: %s (%.0f%% flake rate)\n", entry.Name, entry.FlakeRate*100)
		} else {
			updated++
			fmt.Fprintf(w, "  ↻ updated: %s (%.0f%% flake rate)\n", entry.Name, entry.FlakeRate*100)
		}
	}

	regressions := report.Regressions()
	if len(regressions) > 0 {
		fmt.Fprintf(w, "  ⚠ %d regression(s) detected but NOT quarantined (fix the code, not the test):\n", len(regressions))
		for _, r := range regressions {
			fmt.Fprintf(w, "    ✗ %s (%.0f%%)\n", r.Name, r.FlakeRate()*100)
		}
	}

	if added == 0 && updated == 0 && removed == 0 {
		fmt.Fprintln(w, "No quarantine changes.")
		return nil
	}

	if jiraOpts.Configured() && len(newlyQuarantined) > 0 {
		fileJiraTickets(cfg, newlyQuarantined, jiraOpts, w)
	}

	if err := quarantine.SaveConfig(outPath, cfg); err != nil {
		return err
	}

	fmt.Fprintf(w, "Quarantine config written to %s (%d new, %d refreshed, %d removed)\n", outPath, added, updated, removed)
	return nil
}

func fileJiraTickets(cfg *quarantine.Config, entries []quarantine.Entry, opts *jira.Options, w *os.File) {
	client := jira.NewClient(opts.Server, opts.Token)
	ctx := context.Background()

	for _, entry := range entries {
		summary := fmt.Sprintf("Flaky test quarantined: %s", entry.Name)
		description := fmt.Sprintf(
			"Test %q has been automatically quarantined due to a %.0f%% flake rate "+
				"(%d failures in %d runs over %d days).\n\n"+
				"The test is skipped in CI until this ticket is resolved.\n"+
				"Re-enable date: %s",
			entry.Name, entry.FlakeRate*100,
			entry.FailedRuns, entry.TotalRuns, entry.WindowDays,
			entry.ReEnableAfter,
		)

		result, err := client.CreateIssue(ctx, jira.CreateIssueInput{
			Project:     opts.Project,
			Summary:     summary,
			Description: description,
			Labels:      append([]string{"e2e-flaky-quarantine"}, opts.Labels...),
		})
		if err != nil {
			fmt.Fprintf(w, "  ⚠ failed to create Jira ticket for %s: %v\n", entry.Name, err)
			continue
		}

		cfg.SetJiraKey(entry.Name, result.Key)
		fmt.Fprintf(w, "  ✓ created %s for %s\n", result.Key, entry.Name)
	}
}

func unquarantineResolved(cfg *quarantine.Config, opts *jira.Options, w *os.File) int {
	client := jira.NewClient(opts.Server, opts.Token)
	ctx := context.Background()

	var toRemove []string
	for _, entry := range cfg.Tests {
		if entry.Jira == "" {
			continue
		}
		done, err := client.IsIssueDone(ctx, entry.Jira)
		if err != nil {
			fmt.Fprintf(w, "  ⚠ failed to check %s for %s: %v\n", entry.Jira, entry.Name, err)
			continue
		}
		if done {
			toRemove = append(toRemove, entry.Name)
		}
	}

	for _, name := range toRemove {
		cfg.Remove(name)
		fmt.Fprintf(w, "  ✓ un-quarantined %s (Jira resolved)\n", name)
	}

	return len(toRemove)
}
