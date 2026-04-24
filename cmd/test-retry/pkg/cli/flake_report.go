package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/flakerate"
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
			if threshold < 0 || threshold > 1 {
				return fmt.Errorf("--threshold must be between 0.0 and 1.0, got %.2f", threshold)
			}
			if windowDays <= 0 {
				return fmt.Errorf("--window-days must be > 0, got %d", windowDays)
			}

			report, err := flakerate.AnalyzeDir(junitDir)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			if report.TotalFiles == 0 {
				fmt.Println("No JUnit XML files found in", junitDir)
				return nil
			}

			if outputJSON {
				if err := printJSONReport(report, threshold); err != nil {
					return err
				}
			} else {
				printTextReport(report, threshold, cfg.Verbose)
			}

			if autoQuarantine {
				if quarantineCfgOut == "" {
					return fmt.Errorf("--auto-quarantine requires --quarantine-config")
				}
				return writeQuarantineConfig(report, threshold, windowDays, quarantineCfgOut, outputJSON)
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

func writeQuarantineConfig(report *flakerate.Report, threshold float64, windowDays int, outPath string, jsonMode bool) error {
	w := os.Stdout
	if jsonMode {
		w = os.Stderr
	}

	cfg, err := quarantine.LoadConfig(outPath)
	if err != nil {
		return fmt.Errorf("failed to load existing quarantine config: %w", err)
	}

	newEntries := report.AutoQuarantine(threshold, windowDays)
	added := 0
	updated := 0
	for _, entry := range newEntries {
		alreadyQuarantined, _ := cfg.IsQuarantined(entry.Name)
		cfg.AddOrUpdate(entry)
		if !alreadyQuarantined {
			added++
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

	if added == 0 && updated == 0 {
		fmt.Fprintln(w, "No new tests to quarantine.")
		return nil
	}

	if err := quarantine.SaveConfig(outPath, cfg); err != nil {
		return err
	}

	fmt.Fprintf(w, "Quarantine config written to %s (%d new, %d refreshed)\n", outPath, added, updated)
	return nil
}
