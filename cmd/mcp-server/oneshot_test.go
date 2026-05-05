package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/failureclassifier"
)

func TestDiagnoseReportJSON(t *testing.T) {
	fc := failureclassifier.FailureClassification{
		Category:    "infrastructure",
		Subcategory: "image-pull",
		ErrorCode:   1001,
		Evidence:    []string{"container xyz waiting: ImagePullBackOff"},
		Confidence:  "medium",
	}

	dr := DiagnoseReport{
		Report:         nil,
		Classification: &fc,
		TestName:       "ci-diagnose",
	}

	data, err := json.Marshal(dr)
	if err != nil {
		t.Fatalf("failed to marshal DiagnoseReport: %v", err)
	}

	var decoded DiagnoseReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal DiagnoseReport: %v", err)
	}

	if decoded.TestName != "ci-diagnose" {
		t.Errorf("TestName = %q, want %q", decoded.TestName, "ci-diagnose")
	}
	if decoded.Classification == nil {
		t.Fatal("Classification is nil after round-trip")
	}
	if decoded.Classification.Category != "infrastructure" {
		t.Errorf("Category = %q, want %q", decoded.Classification.Category, "infrastructure")
	}
}

func TestDiagnoseReportJSONWithError(t *testing.T) {
	fc := failureclassifier.FailureClassification{
		Category:    "unknown",
		Subcategory: "collection-error",
		ErrorCode:   0,
		Confidence:  "none",
	}

	dr := DiagnoseReport{
		Report:         nil,
		Classification: &fc,
		TestName:       "fail-test",
		Error:          "clusterhealth.Run error: context deadline exceeded",
	}

	data, err := json.Marshal(dr)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded DiagnoseReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Error == "" {
		t.Error("Error field should be populated after round-trip")
	}
}

func TestRedactString(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"bearer token", "Authorization: Bearer eyJhbGci", "Authorization: Bearer [REDACTED]"},
		{"password", "password=s3cretP@ss", "password=[REDACTED]"},
		{"clean string", "container xyz waiting: ImagePullBackOff", "container xyz waiting: ImagePullBackOff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactString(tt.input); got != tt.want {
				t.Errorf("redactString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRedactEvidence(t *testing.T) {
	fc := failureclassifier.FailureClassification{
		Evidence: []string{
			"Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9",
			"password=s3cretP@ss",
			"api_key=AKIAIOSFODNN7EXAMPLE",
			"clean evidence without secrets",
		},
	}

	redactEvidence(&fc)

	for _, e := range fc.Evidence {
		if e == "clean evidence without secrets" {
			continue
		}
		if !strings.Contains(e, "[REDACTED]") {
			t.Errorf("evidence not redacted: %q", e)
		}
	}
}

func TestRunOneShotWithFakeClient(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	exitCode := runOneShot(newFakeClient(), "fake-test")

	w.Close()

	var buf bytes.Buffer
	io.Copy(&buf, r)

	var dr DiagnoseReport
	if err := json.Unmarshal(buf.Bytes(), &dr); err != nil {
		t.Fatalf("runOneShot output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}

	if dr.Classification == nil {
		t.Fatal("Classification should not be nil")
	}
	if dr.TestName != "fake-test" {
		t.Errorf("TestName = %q, want %q", dr.TestName, "fake-test")
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 for empty fake cluster (missing operator/DSCI/DSC)", exitCode)
	}
	if dr.Report == nil {
		t.Error("Report should not be nil for a completed health check")
	}
	if dr.Error != "" {
		t.Errorf("Error = %q, want empty string", dr.Error)
	}
}
