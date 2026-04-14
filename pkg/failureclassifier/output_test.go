//nolint:testpackage
package failureclassifier

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestEmitClassification(t *testing.T) {
	fc := FailureClassification{
		Category:    CategoryInfrastructure,
		Subcategory: "image-pull",
		ErrorCode:   CodeImagePull,
		Evidence:    []string{"container foo/bar waiting: ImagePullBackOff"},
		Confidence:  ConfidenceMedium,
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	os.Stdout = w

	EmitClassification(fc, "TestSomething")

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("failed to copy stdout: %v", err)
	}
	output := buf.String()

	// Verify the structured line is present
	if !strings.Contains(output, ClassificationPrefix) {
		t.Errorf("stdout should contain %q, got: %s", ClassificationPrefix, output)
	}

	// Extract JSON after prefix
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var jsonLine string
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, ClassificationPrefix); ok {
			jsonLine = after
			break
		}
	}

	if jsonLine == "" {
		t.Fatal("no line with classification prefix found in stdout")
	}

	// Verify valid JSON
	var parsed FailureClassification
	if err := json.Unmarshal([]byte(jsonLine), &parsed); err != nil {
		t.Errorf("JSON unmarshal failed: %v, raw: %s", err, jsonLine)
	}

	if parsed.Category != fc.Category {
		t.Errorf("parsed Category = %q, want %q", parsed.Category, fc.Category)
	}
	if parsed.Subcategory != fc.Subcategory {
		t.Errorf("parsed Subcategory = %q, want %q", parsed.Subcategory, fc.Subcategory)
	}
	if parsed.ErrorCode != fc.ErrorCode {
		t.Errorf("parsed ErrorCode = %d, want %d", parsed.ErrorCode, fc.ErrorCode)
	}
}

func TestFormatJUnitPrefix(t *testing.T) {
	tests := []struct {
		name string
		fc   FailureClassification
		want string
	}{
		{
			name: "infrastructure image-pull",
			fc: FailureClassification{
				Category:    CategoryInfrastructure,
				Subcategory: "image-pull",
			},
			want: "[INFRASTRUCTURE:image-pull]",
		},
		{
			name: "unknown unclassifiable prefix",
			fc: FailureClassification{
				Category:    "custom-category",
				Subcategory: "custom-sub",
			},
			want: "[CUSTOM-CATEGORY:custom-sub]",
		},
		{
			name: "unknown unclassifiable",
			fc: FailureClassification{
				Category:    CategoryUnknown,
				Subcategory: "unclassifiable",
			},
			want: "[UNKNOWN:unclassifiable]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatJUnitPrefix(tt.fc)
			if got != tt.want {
				t.Errorf("FormatJUnitPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJSONRoundTrip(t *testing.T) {
	original := FailureClassification{
		Category:    CategoryInfrastructure,
		Subcategory: "pod-startup",
		ErrorCode:   CodePodStartup,
		Evidence:    []string{"pod test-pod stuck in Pending", "extra evidence"},
		Confidence:  ConfidenceHigh,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var roundTripped FailureClassification
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if roundTripped.Category != original.Category {
		t.Errorf("Category mismatch: got %q, want %q", roundTripped.Category, original.Category)
	}
	if roundTripped.Subcategory != original.Subcategory {
		t.Errorf("Subcategory mismatch: got %q, want %q", roundTripped.Subcategory, original.Subcategory)
	}
	if roundTripped.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode mismatch: got %d, want %d", roundTripped.ErrorCode, original.ErrorCode)
	}
	if roundTripped.Confidence != original.Confidence {
		t.Errorf("Confidence mismatch: got %q, want %q", roundTripped.Confidence, original.Confidence)
	}
	if len(roundTripped.Evidence) != len(original.Evidence) {
		t.Errorf("Evidence length mismatch: got %d, want %d", len(roundTripped.Evidence), len(original.Evidence))
	}
	for i, ev := range roundTripped.Evidence {
		if ev != original.Evidence[i] {
			t.Errorf("Evidence[%d] mismatch: got %q, want %q", i, ev, original.Evidence[i])
		}
	}
}
