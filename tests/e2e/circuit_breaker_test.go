package e2e_test

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/tests/e2e/pkg/failureclassifier"
)

const circuitBreakerSkipPrefix = "CIRCUIT BREAKER OPEN"

type CircuitBreakerState int

const (
	CircuitClosed CircuitBreakerState = iota
	CircuitOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	default:
		return "unknown"
	}
}

// CircuitBreaker halts test execution when repeated infrastructure failures
// are detected. It uses the failure classifier to distinguish infrastructure
// problems from test-logic bugs:
//   - Infrastructure failures (or unknown) increment the consecutive failure counter
//   - Test-logic failures (cluster is healthy) reset the counter
//   - When the counter reaches the threshold, a health check confirms the trip
//
// Each test binary invocation starts with a fresh Closed breaker. The test-retry
// CLI's retry mechanism naturally provides "half-open" probing on subsequent runs.
type CircuitBreaker struct {
	mu                  sync.Mutex
	state               CircuitBreakerState
	consecutiveFailures int
	threshold           int
	healthChecker       *ClusterHealthChecker
	tripReason          string
	totalTrips          int
}

var circuitBreaker *CircuitBreaker

func NewCircuitBreaker(threshold int, healthChecker *ClusterHealthChecker) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if healthChecker == nil {
		log.Printf("Circuit breaker: WARNING — no health checker provided, health checks will be skipped")
	}
	return &CircuitBreaker{
		state:         CircuitClosed,
		threshold:     threshold,
		healthChecker: healthChecker,
	}
}

// On failure, the classifier (populated by HandleTestFailure before this call)
// determines the category:
//   - CategoryTest: cluster is healthy, failure is test logic — reset counter
//   - CategoryInfrastructure or CategoryUnknown: increment counter
func (cb *CircuitBreaker) RecordResult(passed bool, classification *atomic.Pointer[failureclassifier.FailureClassification]) {
	if cb == nil {
		return
	}
	cb.mu.Lock()

	if cb.state == CircuitOpen {
		cb.mu.Unlock()
		return
	}

	if passed {
		if cb.consecutiveFailures > 0 {
			log.Printf("Circuit breaker: success recorded, resetting failure counter (was %d)", cb.consecutiveFailures)
		}
		cb.consecutiveFailures = 0
		cb.mu.Unlock()
		return
	}

	// Check the failure classification to decide if this failure counts
	if classification != nil {
		if fc := classification.Load(); fc != nil {
			if fc.Category == failureclassifier.CategoryTest {
				log.Printf("Circuit breaker: failure classified as test-logic (%s/%s, code=%d) — not counting toward threshold",
					fc.Category, fc.Subcategory, fc.ErrorCode)
				cb.consecutiveFailures = 0
				cb.mu.Unlock()
				return
			}
			log.Printf("Circuit breaker: failure classified as %s/%s (code=%d)",
				fc.Category, fc.Subcategory, fc.ErrorCode)
		}
	}

	cb.consecutiveFailures++
	log.Printf("Circuit breaker: infrastructure/unknown failure recorded (%d/%d consecutive)",
		cb.consecutiveFailures, cb.threshold)

	needsHealthCheck := cb.consecutiveFailures >= cb.threshold
	failures := cb.consecutiveFailures
	cb.mu.Unlock()

	if needsHealthCheck && cb.healthChecker != nil {
		cb.evaluateHealth(failures)
	}
}

// evaluateHealth runs the cluster health check outside the mutex,
// then re-acquires it to apply the result atomically.
func (cb *CircuitBreaker) evaluateHealth(failures int) {
	log.Printf("Circuit breaker: threshold reached (%d failures), running confirming health check...",
		failures)

	health := cb.healthChecker.Check()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitOpen {
		return
	}

	if !health.Healthy {
		cb.state = CircuitOpen
		cb.totalTrips++
		cb.tripReason = fmt.Sprintf(
			"Infrastructure failure detected after %d consecutive infrastructure/unknown failures. "+
				"Cluster health issues: [%s]",
			failures,
			strings.Join(health.Issues, "; "))
		log.Printf("CIRCUIT BREAKER TRIPPED: %s", cb.tripReason)
	} else {
		log.Printf("Circuit breaker: confirming health check passed — cluster appears healthy, resetting counter")
		cb.consecutiveFailures = 0
	}
}

// IsOpen returns true if the circuit breaker has tripped.
func (cb *CircuitBreaker) IsOpen() bool {
	if cb == nil {
		return false
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state == CircuitOpen
}

// TripReason returns the reason why the breaker tripped.
func (cb *CircuitBreaker) TripReason() string {
	if cb == nil {
		return ""
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.tripReason
}

// TotalTrips returns the number of times the breaker has tripped in this run.
func (cb *CircuitBreaker) TotalTrips() int {
	if cb == nil {
		return 0
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.totalTrips
}

// SkipIfOpen skips the current test if the breaker is open.
func (cb *CircuitBreaker) SkipIfOpen(t *testing.T) bool {
	t.Helper()
	if cb == nil {
		return false
	}
	if cb.IsOpen() {
		t.Skipf("%s: %s", circuitBreakerSkipPrefix, cb.TripReason())
		return true
	}
	return false
}

// ForceTrip immediately opens the circuit breaker with the given reason,
// bypassing the consecutive-failure threshold. Used by the pre-flight check.
func (cb *CircuitBreaker) ForceTrip(reason string) {
	if cb == nil {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitOpen {
		return
	}

	cb.state = CircuitOpen
	cb.totalTrips++
	cb.tripReason = reason
	log.Printf("CIRCUIT BREAKER TRIPPED: %s", reason)
}

// LogSummary prints a summary of circuit breaker activity.
func (cb *CircuitBreaker) LogSummary() {
	if cb == nil {
		return
	}
	trips := cb.TotalTrips()
	if trips == 0 {
		log.Printf("Circuit breaker: no trips during this run (threshold=%d)", cb.threshold)
		return
	}

	log.Printf("=== CIRCUIT BREAKER SUMMARY ===")
	log.Printf("Tripped %d time(s)", trips)
	log.Printf("Reason: %s", cb.TripReason())
	log.Printf("===============================")
}
