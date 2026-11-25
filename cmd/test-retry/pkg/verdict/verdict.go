package verdict

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	statusPassed = "passed"
	statusFailed = "failed"

	domainPlatformOperator    = "platform-operator"
	domainModuleConfiguration = "module-configuration"
	domainTestInfrastructure  = "test-infrastructure"
	domainUnknown             = "unknown"
)

// Failure is an attributed production DAG failure.
type Failure struct {
	Module                 string `json:"module,omitempty"`
	PossibleModuleFailure  bool   `json:"possibleModuleFailure,omitempty"`
	Domain                 string `json:"domain"`
	Stage                  string `json:"stage,omitempty"`
	LastSuccessfulBoundary string `json:"lastSuccessfulBoundary,omitempty"`
	Message                string `json:"message,omitempty"`
}

// Report contains failures attributed by the DAG diagnostics.
type Report struct {
	Failures []Failure `json:"failures"`
}

// Result is the policy-aware aggregate result of core and DAG E2E phases.
type Result struct {
	Mode            string    `json:"mode"`
	CoreStatus      string    `json:"coreStatus"`
	DAGStatus       string    `json:"dagStatus"`
	OperatorVerdict string    `json:"operatorVerdict"`
	Reason          string    `json:"reason"`
	Failures        []Failure `json:"failures,omitempty"`
}

// ReadReport reads an optional DAG attribution report. Missing or malformed
// reports are returned as nil so a failed DAG remains blocking.
func ReadReport(path string) *Report {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil
	}
	return &report
}

// Evaluate applies the E2E policy to core and DAG process results.
func Evaluate(coreExit, dagExit int, report *Report, coreOnly bool) (Result, int) {
	mode := "all"
	if coreOnly {
		mode = "core"
	}
	if coreExit != 0 {
		return Result{
			Mode:            mode,
			CoreStatus:      statusFailed,
			DAGStatus:       "not-run",
			OperatorVerdict: statusFailed,
			Reason:          "core or synthetic platform contract failed",
		}, coreExit
	}
	if coreOnly {
		return Result{
			Mode:            mode,
			CoreStatus:      statusPassed,
			DAGStatus:       "not-run",
			OperatorVerdict: statusPassed,
			Reason:          "core and synthetic platform contracts passed; production DAG was not requested",
		}, 0
	}
	if dagExit == 0 {
		return Result{
			Mode:            mode,
			CoreStatus:      statusPassed,
			DAGStatus:       statusPassed,
			OperatorVerdict: statusPassed,
			Reason:          "core, synthetic, and production DAG passed",
		}, 0
	}
	if dagExit < 0 {
		return Result{
			Mode:            mode,
			CoreStatus:      statusPassed,
			DAGStatus:       "not-run",
			OperatorVerdict: statusFailed,
			Reason:          "DAG was not run",
		}, 1
	}

	if report == nil || len(report.Failures) == 0 {
		return Result{
			Mode:            mode,
			CoreStatus:      statusPassed,
			DAGStatus:       statusFailed,
			OperatorVerdict: statusFailed,
			Reason:          "DAG failure has no trustworthy attribution",
		}, dagExit
	}

	allModuleConfiguration := true
	hasTentativeModuleAttribution := false
	for _, failure := range report.Failures {
		if failure.PossibleModuleFailure {
			hasTentativeModuleAttribution = true
		}
		if failure.Domain != domainModuleConfiguration ||
			strings.TrimSpace(failure.Module) == "" ||
			strings.TrimSpace(failure.Stage) == "" ||
			strings.TrimSpace(failure.LastSuccessfulBoundary) == "" ||
			strings.TrimSpace(failure.Message) == "" {
			allModuleConfiguration = false
		}
	}
	if hasTentativeModuleAttribution && (len(report.Failures) != 1 || !isTentativeModuleStage(report.Failures[0].Stage)) {
		allModuleConfiguration = false
	}
	if allModuleConfiguration {
		reason := "operator-owned contracts passed; DAG failures are attributed only to module configuration"
		if hasTentativeModuleAttribution {
			reason = "operator-owned contracts passed; a single unhealthy managed module was tentatively attributed as the DAG failure"
		}
		return Result{
			Mode:            mode,
			CoreStatus:      statusPassed,
			DAGStatus:       "failed-module-configuration",
			OperatorVerdict: "passed-with-integration-warning",
			Reason:          reason,
			Failures:        report.Failures,
		}, 0
	}

	return Result{
		Mode:            mode,
		CoreStatus:      statusPassed,
		DAGStatus:       statusFailed,
		OperatorVerdict: statusFailed,
		Reason:          "DAG contains platform, infrastructure, unknown, or invalidly attributed failures",
		Failures:        report.Failures,
	}, dagExit
}

func isTentativeModuleStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case "status", "crd-establishment", "controller-deployment", "module-status", "release-handshake":
		return true
	default:
		return false
	}
}

// Print writes a human-readable E2E summary.
func Print(w io.Writer, result Result) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "E2E RESULT SUMMARY")
	fmt.Fprintf(w, "  MODE:             %s\n", result.Mode)
	fmt.Fprintf(w, "  CORE + SYNTHETIC: %s\n", result.CoreStatus)
	fmt.Fprintf(w, "  PRODUCTION DAG:   %s\n", result.DAGStatus)
	fmt.Fprintf(w, "  OPERATOR VERDICT: %s\n", result.OperatorVerdict)
	fmt.Fprintf(w, "  REASON:           %s\n", result.Reason)
	for _, failure := range result.Failures {
		fmt.Fprintf(w, "  DAG FAILURE: module=%s possibleModuleFailure=%t domain=%s stage=%s boundary=%s message=%s\n",
			failure.Module, failure.PossibleModuleFailure, failure.Domain, failure.Stage, failure.LastSuccessfulBoundary, failure.Message)
	}
}

// WriteResult writes the machine-readable E2E summary.
func WriteResult(path string, result Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
