package dag

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrUnknownNode is returned by a ReadinessChecker when it does not
// recognize the named node. CompositeChecker uses this to try the
// next checker in the chain.
var ErrUnknownNode = errors.New("unknown node")

// Runlevel determines provisioning order. Lower values provision first.
// Nodes at the same level form a single batch; all must be Ready before
// the next level begins. Use dag.RL(n) to construct.
type Runlevel struct {
	Order int
}

func (r Runlevel) String() string {
	return fmt.Sprintf("%02d", r.Order)
}

// RL constructs a Runlevel with the given order.
// Use any value; lower orders provision first.
//
//	dag.RL(20)  // core AI/ML components
//	dag.RL(31)  // first extension sub-tier
//	dag.RL(32)  // second extension sub-tier
func RL(order int) Runlevel {
	return Runlevel{Order: order}
}

// Node is the minimal interface for DAG participation.
type Node interface {
	GetName() string
	GetRunlevel() Runlevel
}

// RunlevelPolicy configures deadlock-avoidance behavior per runlevel.
type RunlevelPolicy struct {
	// Timeout is the wall-clock duration a runlevel can remain not-ready
	// before the orchestrator advances past it. 0 = block forever.
	Timeout time.Duration
}

// Graph holds nodes and resolves them into runlevel-grouped, topologically
// sorted batches. T must satisfy the Node interface.
type Graph[T Node] struct {
	nodes map[string]T
}

// NewGraph creates an empty Graph.
func NewGraph[T Node]() *Graph[T] {
	return &Graph[T]{nodes: make(map[string]T)}
}

// Add inserts a node into the graph. Duplicate names overwrite.
func (g *Graph[T]) Add(node T) {
	g.nodes[node.GetName()] = node
}

// Resolve groups nodes by runlevel order and returns a slice of batches
// (one per distinct runlevel, ascending). Within each batch, nodes are
// sorted alphabetically for determinism.
func (g *Graph[T]) Resolve() ([][]T, error) {
	if len(g.nodes) == 0 {
		return nil, nil
	}

	groups := g.groupByRunlevel()

	keys := make([]int, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	batches := make([][]T, 0, len(keys))
	for _, order := range keys {
		batch := groups[order]
		sort.Slice(batch, func(i, j int) bool {
			return batch[i].GetName() < batch[j].GetName()
		})
		batches = append(batches, batch)
	}

	return batches, nil
}

// ReverseBatches returns batches in reverse order, with each batch's
// internal order also reversed. Use for cleanup (higher runlevels first).
func (g *Graph[T]) ReverseBatches() ([][]T, error) {
	batches, err := g.Resolve()
	if err != nil {
		return nil, err
	}

	for i, j := 0, len(batches)-1; i < j; i, j = i+1, j-1 {
		batches[i], batches[j] = batches[j], batches[i]
	}
	for _, batch := range batches {
		for i, j := 0, len(batch)-1; i < j; i, j = i+1, j-1 {
			batch[i], batch[j] = batch[j], batch[i]
		}
	}

	return batches, nil
}

// groupByRunlevel groups nodes by their Order, producing separate
// batches for each distinct runlevel.
func (g *Graph[T]) groupByRunlevel() map[int][]T {
	groups := make(map[int][]T)
	for _, node := range g.nodes {
		key := node.GetRunlevel().Order
		groups[key] = append(groups[key], node)
	}
	return groups
}

// DefaultTimeout is the wall-clock duration a runlevel can remain
// not-ready before the orchestrator advances past it.
const DefaultTimeout = 10 * time.Minute

var (
	runlevelPolicyMu        sync.RWMutex
	runlevelPolicyOverrides = map[int]RunlevelPolicy{}
)

// SetRunlevelPolicy sets a per-runlevel timeout override.
// Use Timeout: 0 to block forever (never skip).
func SetRunlevelPolicy(order int, policy RunlevelPolicy) {
	runlevelPolicyMu.Lock()
	defer runlevelPolicyMu.Unlock()

	runlevelPolicyOverrides[order] = policy
}

// ClearRunlevelPolicy removes a per-runlevel override, reverting to
// DefaultTimeout.
func ClearRunlevelPolicy(order int) {
	runlevelPolicyMu.Lock()
	defer runlevelPolicyMu.Unlock()

	delete(runlevelPolicyOverrides, order)
}

// GetRunlevelPolicy returns the policy for the given runlevel order.
// It checks overrides first, falling back to DefaultTimeout.
func GetRunlevelPolicy(order int) RunlevelPolicy {
	runlevelPolicyMu.RLock()
	defer runlevelPolicyMu.RUnlock()

	if p, ok := runlevelPolicyOverrides[order]; ok {
		return p
	}
	return RunlevelPolicy{Timeout: DefaultTimeout}
}

// FormatDuration formats a duration for human-readable condition messages.
//
//	10m0s  → "10 minutes"
//	1h0m0s → "1 hour"
//	1h30m0s → "1h30m"
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "indefinitely"
	}
	if d%time.Hour == 0 && d/time.Hour == 1 {
		return "1 hour"
	}
	if d%time.Minute == 0 {
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	}
	return d.Truncate(time.Second).String()
}

// StuckTracker records the wall-clock time each runlevel first became
// stuck for a given instance. Timestamps are stored in-memory and reset
// on pod restart, which is acceptable since the cluster state may have
// changed. The controller is single-threaded (MaxConcurrentReconciles=1),
// so no synchronization is needed.
type StuckTracker struct {
	timestamps map[stuckKey]time.Time
}

type stuckKey struct {
	instance      string
	runlevelOrder int
}

func NewStuckTracker() *StuckTracker {
	return &StuckTracker{timestamps: make(map[stuckKey]time.Time)}
}

// Since returns the time the given runlevel first became stuck for the
// instance identified by instanceID. On first call for a key, records
// time.Now() and returns it.
func (t *StuckTracker) Since(instanceID string, runlevelOrder int) time.Time {
	k := stuckKey{instance: instanceID, runlevelOrder: runlevelOrder}
	ts, ok := t.timestamps[k]
	if !ok {
		ts = time.Now()
		t.timestamps[k] = ts
	}
	return ts
}

// Clear removes the stuck timestamp for a runlevel, indicating it is
// no longer blocked.
func (t *StuckTracker) Clear(instanceID string, runlevelOrder int) {
	delete(t.timestamps, stuckKey{instance: instanceID, runlevelOrder: runlevelOrder})
}

// ReadinessChecker determines whether a named node is ready for DAG gating.
type ReadinessChecker interface {
	IsReady(ctx context.Context, name string) (bool, error)
}

// CompositeChecker dispatches readiness checks across multiple checkers.
// The first checker that recognizes the name wins.
type CompositeChecker []ReadinessChecker

// IsReady tries each checker in order; returns the result from the first
// one that doesn't return ErrUnknownNode. If a checker returns
// ErrUnknownNode, the next checker is tried. Any other error is
// returned immediately.
func (c CompositeChecker) IsReady(ctx context.Context, name string) (bool, error) {
	for _, checker := range c {
		ready, err := checker.IsReady(ctx, name)
		if errors.Is(err, ErrUnknownNode) {
			continue
		}
		if err != nil {
			return false, err
		}
		return ready, nil
	}
	return false, fmt.Errorf("no checker recognizes node %q: %w", name, ErrUnknownNode)
}
