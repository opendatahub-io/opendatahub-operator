package e2e_test

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
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

// CircuitBreaker halts test execution when infrastructure failures are detected.
//
// It tracks consecutive test failures at the mustRun scope (per-suite). When the
// failure count reaches the configured threshold, it runs a cluster health check
// to distinguish infrastructure failures from test logic failures:
//   - If the cluster is unhealthy → the breaker trips (Open), skipping remaining tests
//   - If the cluster is healthy  → the counter resets (failures are test logic)
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

// RecordResult upon reaching the consecutive failures threshold, triggers a health check.
func (cb *CircuitBreaker) RecordResult(passed bool) {
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

	cb.consecutiveFailures++
	log.Printf("Circuit breaker: failure recorded (%d/%d consecutive)",
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
	log.Printf("Circuit breaker: threshold reached (%d failures), running cluster health check...",
		failures)

	health := cb.healthChecker.Check()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Re-check: another goroutine may have tripped or reset while we were checking.
	if cb.state == CircuitOpen {
		return
	}

	if !health.Healthy {
		cb.state = CircuitOpen
		cb.totalTrips++
		cb.tripReason = fmt.Sprintf(
			"Infrastructure failure detected after %d consecutive test failures. "+
				"Cluster health issues: [%s]",
			failures,
			strings.Join(health.Issues, "; "))
		log.Printf("CIRCUIT BREAKER TRIPPED: %s", cb.tripReason)
	} else {
		log.Printf("Circuit breaker: cluster is healthy — failures are test logic, resetting counter")
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
