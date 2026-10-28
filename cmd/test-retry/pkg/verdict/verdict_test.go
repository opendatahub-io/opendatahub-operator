package verdict

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validFailure() Failure {
	return Failure{
		Module:                 "kserve",
		Domain:                 domainModuleConfiguration,
		Stage:                  "status",
		LastSuccessfulBoundary: "operator-ready",
		Message:                "module deployment unavailable",
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name       string
		coreExit   int
		dagExit    int
		report     *Report
		wantCode   int
		wantState  string
		wantDAG    string
		wantReason string
	}{
		{name: "core failure", coreExit: 2, dagExit: -1, wantCode: 2, wantState: "failed", wantDAG: "not-run"},
		{name: "all pass", dagExit: 0, wantState: "passed", wantDAG: "passed"},
		{name: "DAG not run is blocking by default", dagExit: -1, wantCode: 1, wantState: "failed", wantDAG: "not-run"},
		{name: "missing report is blocking", dagExit: 1, wantCode: 1, wantState: "failed", wantDAG: "failed"},
		{name: "empty report is blocking", dagExit: 1, report: &Report{}, wantCode: 1, wantState: "failed", wantDAG: "failed"},
		{
			name:    "complete module configuration failures are warnings",
			dagExit: 1,
			report: &Report{Failures: []Failure{
				validFailure(),
				{
					Module:                 "ray",
					Domain:                 domainModuleConfiguration,
					Stage:                  "status",
					LastSuccessfulBoundary: "deployed",
					Message:                "not ready",
				},
			}},
			wantState: "passed-with-integration-warning",
			wantDAG:   "failed-module-configuration",
		},
		{
			name:    "single complete tentative attribution is warning",
			dagExit: 1,
			report: func() *Report {
				failure := validFailure()
				failure.PossibleModuleFailure = true
				return &Report{Failures: []Failure{failure}}
			}(),
			wantState:  "passed-with-integration-warning",
			wantDAG:    "failed-module-configuration",
			wantReason: "operator-owned contracts passed; a single unhealthy managed module was tentatively attributed as the DAG failure",
		},
		{
			name:    "tentative attribution with non-status stage is blocking",
			dagExit: 6,
			report: func() *Report {
				failure := validFailure()
				failure.PossibleModuleFailure = true
				failure.Stage = "deployment"
				return &Report{Failures: []Failure{failure}}
			}(),
			wantCode: 6, wantState: "failed", wantDAG: "failed",
		},
		{
			name:    "tentative attribution among multiple failures is blocking",
			dagExit: 4,
			report: func() *Report {
				tentative := validFailure()
				tentative.PossibleModuleFailure = true
				other := validFailure()
				other.Module = "ray"
				return &Report{Failures: []Failure{tentative, other}}
			}(),
			wantCode: 4, wantState: "failed", wantDAG: "failed",
		},
		{
			name:      "platform failure is blocking",
			dagExit:   1,
			report:    &Report{Failures: []Failure{{Domain: domainPlatformOperator}}},
			wantCode:  1,
			wantState: "failed",
			wantDAG:   "failed",
		},
		{
			name:    "mixed module and platform failures are blocking",
			dagExit: 2,
			report: &Report{Failures: []Failure{
				validFailure(),
				{Domain: domainPlatformOperator, Stage: "dag", Message: "operator failure"},
			}},
			wantCode:  2,
			wantState: "failed",
			wantDAG:   "failed",
		},
		{
			name:      "infrastructure failure is blocking",
			dagExit:   3,
			report:    &Report{Failures: []Failure{{Domain: domainTestInfrastructure}}},
			wantCode:  3,
			wantState: "failed",
			wantDAG:   "failed",
		},
		{name: "unknown failure is blocking", dagExit: 1, report: &Report{Failures: []Failure{{Domain: domainUnknown}}}, wantCode: 1, wantState: "failed", wantDAG: "failed"},
		{name: "unrecognized domain is blocking", dagExit: 1, report: &Report{Failures: []Failure{{Domain: "other"}}}, wantCode: 1, wantState: "failed", wantDAG: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, code := Evaluate(tt.coreExit, tt.dagExit, tt.report, false)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantState, got.OperatorVerdict)
			assert.Equal(t, tt.wantDAG, got.DAGStatus)
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, got.Reason)
			}
		})
	}
}

func TestEvaluateAcceptsStructuredTentativeModuleStages(t *testing.T) {
	for _, stage := range []string{"status", "crd-establishment", "controller-deployment", "module-status", "release-handshake"} {
		t.Run(stage, func(t *testing.T) {
			failure := validFailure()
			failure.PossibleModuleFailure = true
			failure.Stage = stage
			got, code := Evaluate(0, 1, &Report{Failures: []Failure{failure}}, false)
			assert.Zero(t, code)
			assert.Equal(t, "passed-with-integration-warning", got.OperatorVerdict)
		})
	}
}

func TestEvaluateRejectsIncompleteModuleConfigurationFailure(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Failure)
	}{
		{name: "module missing", mutate: func(f *Failure) { f.Module = "" }},
		{name: "module whitespace", mutate: func(f *Failure) { f.Module = "  " }},
		{name: "stage missing", mutate: func(f *Failure) { f.Stage = "" }},
		{name: "stage whitespace", mutate: func(f *Failure) { f.Stage = "\t" }},
		{name: "boundary missing", mutate: func(f *Failure) { f.LastSuccessfulBoundary = "" }},
		{name: "boundary whitespace", mutate: func(f *Failure) { f.LastSuccessfulBoundary = "\n" }},
		{name: "message missing", mutate: func(f *Failure) { f.Message = "" }},
		{name: "message whitespace", mutate: func(f *Failure) { f.Message = "   " }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := validFailure()
			tt.mutate(&failure)
			got, code := Evaluate(0, 7, &Report{Failures: []Failure{failure}}, false)
			assert.Equal(t, 7, code)
			assert.Equal(t, "failed", got.OperatorVerdict)
		})
	}
}

func TestEvaluateCoreOnly(t *testing.T) {
	got, code := Evaluate(0, -1, nil, true)
	assert.Equal(t, 0, code)
	assert.Equal(t, "passed", got.OperatorVerdict)
	assert.Equal(t, "core", got.Mode)
	assert.Equal(t, "not-run", got.DAGStatus)
	assert.Contains(t, got.Reason, "not requested")

	got, code = Evaluate(5, -1, nil, true)
	assert.Equal(t, 5, code)
	assert.Equal(t, "failed", got.OperatorVerdict)
	assert.Equal(t, "core", got.Mode)
}

func TestReadDAGReportMalformedAndMissingAreUntrusted(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.json")
	empty := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(malformed, []byte(`{"failures":`), 0o600))
	require.NoError(t, os.WriteFile(empty, nil, 0o600))

	assert.Nil(t, ReadReport(""))
	assert.Nil(t, ReadReport(filepath.Join(dir, "missing.json")))
	assert.Nil(t, ReadReport(malformed))
	assert.Nil(t, ReadReport(empty))
}

func TestReadDAGReportValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	data := []byte(`{"failures":[{"module":"kserve","domain":"module-configuration",` +
		`"stage":"deployment","lastSuccessfulBoundary":"ready","message":"unavailable"}]}`)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	report := ReadReport(path)
	require.NotNil(t, report)
	require.Len(t, report.Failures, 1)
	assert.Equal(t, validFailure().Module, report.Failures[0].Module)
}
