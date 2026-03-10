package classifier

import "strings"

// Category constants for failure classification.
const (
	CategoryInfrastructure = "infrastructure"
	CategoryTest           = "test"
	CategoryUnknown        = "unknown"
)

// Confidence levels for classification results.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Error code constants.
//
// Ranges:
//
//	1000-1999: infrastructure failures
//	2000-2999: test failures
//	3000+:     unknown/unclassifiable
const (
	CodeImagePull      = 1001
	CodePodStartup     = 1002
	CodeNetwork        = 1003
	CodeQuotaOOM       = 1004
	CodeNodePressure   = 1005
	CodeStorage        = 1006
	CodeContainerOOM   = 1007
	CodeOperator       = 1008
	CodeDSCI           = 1009
	CodeDSC            = 1010
	CodeInfraUnknown   = 1099
	CodeTestFailure    = 2001
	CodeUnclassifiable = 3000
)

// FailureClassification categorizes a test failure based on cluster state inspection.
type FailureClassification struct {
	Category    string   `json:"category"`    // "infrastructure", "test", or "unknown"
	Subcategory string   `json:"subcategory"` // e.g., "image-pull", "pod-startup"
	ErrorCode   int      `json:"error_code"`  // see code constants above
	Evidence    []string `json:"evidence"`    // supporting data points
	Confidence  string   `json:"confidence"`  // "high", "medium", or "low"
}

// classificationPattern maps a substring pattern to a subcategory and error code.
type classificationPattern struct {
	pattern     string // substring to match in the message text
	subcategory string
	errorCode   int
}

// Patterns matched against container Waiting message text (substring match, case-insensitive).
// Checked in order; first match wins.
var waitingPatterns = []classificationPattern{
	{"ImagePullBackOff", "image-pull", CodeImagePull},
	{"ErrImagePull", "image-pull", CodeImagePull},
	{"InvalidImageName", "image-pull", CodeImagePull},
	{"pulling image", "image-pull", CodeImagePull},
	{"CrashLoopBackOff", "pod-startup", CodePodStartup},
	{"CreateContainerConfigError", "pod-startup", CodePodStartup},
	{"CreateContainerError", "pod-startup", CodePodStartup},
}

// Patterns matched against container Terminated message text (substring match).
var terminatedPatterns = []classificationPattern{
	{"OOMKilled", "container-oom", CodeContainerOOM},
}

// Event reason patterns indicating network issues (exact match on event Reason field).
var networkEventReasons = map[string]bool{
	"NetworkNotReady": true,
}

// Network message patterns matched against event Message text (substring match, case-insensitive).
var networkMessagePatterns = []string{
	"network not ready",
}

// Event reason patterns indicating storage/PVC issues (exact match on event Reason field).
var storageEventReasons = map[string]bool{
	"FailedAttachVolume": true,
	"FailedMount":        true,
	"ProvisioningFailed": true,
}

// Storage message patterns matched against event Message text (substring match, case-insensitive).
var storageMessagePatterns = []string{
	"persistentvolumeclaim",
	"pvc not found",
	"failed to attach volume",
	"failed to mount volume",
	"no persistent volumes available",
}

// matchesPattern checks if text contains any pattern (case-insensitive).
// Returns the first matching pattern or nil.
func matchesPattern(text string, patterns []classificationPattern) *classificationPattern {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	for i := range patterns {
		if strings.Contains(lower, strings.ToLower(patterns[i].pattern)) {
			return &patterns[i]
		}
	}
	return nil
}

// containsNetworkPattern checks if text contains any network-related pattern (case-insensitive).
func containsNetworkPattern(text string) bool {
	return containsAnyPattern(text, networkMessagePatterns)
}

// containsStoragePattern checks if text contains any storage-related pattern (case-insensitive).
func containsStoragePattern(text string) bool {
	return containsAnyPattern(text, storageMessagePatterns)
}

// containsAnyPattern checks if text contains any of the given patterns (case-insensitive).
func containsAnyPattern(text string, patterns []string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
