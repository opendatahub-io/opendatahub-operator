package classifier

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ClassificationPrefix is the structured line prefix that test-retry parses from stdout.
const ClassificationPrefix = "FAILURE_CLASSIFICATION: "

// EmitClassification writes classification output in two forms:
//  1. Human-readable summary via log.Printf (stderr, for CI logs)
//  2. Structured JSON line via fmt.Println (stdout, for test-retry/machine consumption)
//
// fmt.Println is required (not log.Printf or t.Log) because test-retry's
// gotestsum parser discards stderr and t.Log scoping complicates parsing.
func EmitClassification(fc FailureClassification, testName string) {
	// Human-readable summary to stderr
	log.Printf("Classification for %s: %s/%s (code=%d, confidence=%s)",
		testName, fc.Category, fc.Subcategory, fc.ErrorCode, fc.Confidence)
	if len(fc.Evidence) > 0 {
		log.Printf("  Evidence: %s", strings.Join(fc.Evidence, "; "))
	}

	// Structured JSON line to stdout for machine consumption
	jsonBytes, err := json.Marshal(fc)
	if err != nil {
		log.Printf("ERROR: failed to marshal classification: %v", err)
		return
	}
	fmt.Println(ClassificationPrefix + string(jsonBytes))
}

// FormatJUnitPrefix returns a bracket-formatted string suitable for prefixing
// JUnit test names, e.g. "[INFRASTRUCTURE:image-pull]".
func FormatJUnitPrefix(fc FailureClassification) string {
	return fmt.Sprintf("[%s:%s]", strings.ToUpper(fc.Category), fc.Subcategory)
}
