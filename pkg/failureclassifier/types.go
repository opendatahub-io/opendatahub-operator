package failureclassifier

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
	CodeRBAC           = 1011
	CodeDNS            = 1012
	CodeTimeout        = 1013
	CodeProbeFailure   = 1014
	CodeInvalidFlags   = 1015
	CodeContainerExit  = 1016
	CodeInfraUnknown   = 1099
	CodeTestFailure    = 2001
	CodeNoSignal       = 3001
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
// NOTE: "pulling image" is intentionally absent — it fires during normal startup, not just on errors.
var waitingPatterns = []classificationPattern{
	{"ImagePullBackOff", "image-pull", CodeImagePull},
	{"ErrImagePull", "image-pull", CodeImagePull},
	{"InvalidImageName", "image-pull", CodeImagePull},
	{"CrashLoopBackOff", "pod-startup", CodePodStartup},
	{"CreateContainerConfigError", "pod-startup", CodePodStartup},
	{"CreateContainerError", "pod-startup", CodePodStartup},
}

// Patterns matched against container Terminated message text (substring match, case-insensitive).
// Checked in order; first match wins — OOMKilled must remain first so it takes priority over exit-code patterns.
// Exit-code patterns are only evaluated when the termination is non-graceful (see isGracefulTermination);
// graceful SIGTERM/drain cases have Kubernetes reason "Completed" and must not be classified as failures.
var terminatedPatterns = []classificationPattern{
	{"OOMKilled", "container-oom", CodeContainerOOM},
	{"(exit 2)", "invalid-cli-flags", CodeInvalidFlags},
	{"(exit 137)", "container-exit", CodeContainerExit},
	{"(exit 143)", "container-exit", CodeContainerExit},
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

// RBAC message patterns matched against event Message text (substring match, case-insensitive).
// RBAC errors have no single k8s event Reason; the signal is always in the message.
// "unauthorized" is intentionally absent — it is HTTP 401 (authentication failure) not HTTP 403
// (RBAC denial), and fires on registry auth errors like "unauthorized: authentication required".
// Registry-auth "unauthorized" events are caught by imagePullEventPatterns instead.
var rbacMessagePatterns = []string{
	"is forbidden",
	"access denied",
	"does not have permission",
}

// Image pull event message patterns matched against event Message text (substring match, case-insensitive).
// Covers registry auth failures that surface as events rather than pod waiting states.
var imagePullEventPatterns = []string{
	"unauthorized: authentication",
	"failed to pull image",
	"failed to pull and unpack image",
}

// DNS message patterns matched against event Message text (substring match, case-insensitive).
// Patterns are intentionally specific to DNS resolution errors only — generic TCP patterns
// like "dial tcp" or "connection refused" are excluded because they fire on any network error.
var dnsMessagePatterns = []string{
	"no such host",
	"failed to resolve",
	"dns resolution failed",
	"name or service not known",
	"temporary failure in name resolution",
}

// Timeout message patterns matched against event Message text (substring match, case-insensitive).
// Use "timed out waiting" to match only k8s deadline timeout messages.
var timeoutMessagePatterns = []string{
	"context deadline exceeded",
	"deadline exceeded",
	"timed out waiting",
	"operation timed out",
}

// Event reason patterns indicating liveness/readiness probe failures (exact match on event Reason field).
var probeEventReasons = map[string]bool{
	"Unhealthy": true,
}

// Probe message patterns matched against event Message text (substring match, case-insensitive).
var probeMessagePatterns = []string{
	"liveness probe failed",
	"readiness probe failed",
	"startup probe failed",
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

// containsImagePullEventPattern checks if text contains any image-pull event pattern (case-insensitive).
func containsImagePullEventPattern(text string) bool {
	return containsAnyPattern(text, imagePullEventPatterns)
}

// containsRBACPattern checks if text contains any RBAC-related pattern (case-insensitive).
func containsRBACPattern(text string) bool {
	return containsAnyPattern(text, rbacMessagePatterns)
}

// containsDNSPattern checks if text contains any DNS-related pattern (case-insensitive).
func containsDNSPattern(text string) bool {
	return containsAnyPattern(text, dnsMessagePatterns)
}

// containsTimeoutPattern checks if text contains any timeout-related pattern (case-insensitive).
func containsTimeoutPattern(text string) bool {
	return containsAnyPattern(text, timeoutMessagePatterns)
}

// containsProbePattern checks if text contains any probe-failure pattern (case-insensitive).
func containsProbePattern(text string) bool {
	return containsAnyPattern(text, probeMessagePatterns)
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
