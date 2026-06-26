package provision

import (
	"sync"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// NodeKind distinguishes components from modules in the unified DAG.
type NodeKind string

const (
	KindComponent NodeKind = "component"
	KindModule    NodeKind = "module"
)

// UnifiedNode is a DAG entry that can represent either a component or a
// module. It implements dag.Node so both types participate in the same
// graph resolution.
type UnifiedNode struct {
	name     string
	kind     NodeKind
	runlevel dag.Runlevel
	enabled  bool
}

func (n UnifiedNode) GetName() string           { return n.name }
func (n UnifiedNode) GetRunlevel() dag.Runlevel { return n.runlevel }
func (n UnifiedNode) GetKind() NodeKind         { return n.kind }

// UnifiedRegistry merges component and module DAG metadata into a single
// graph. Both controllers resolve the same unified batches so ordering
// and readiness gating span the full provisioning tree.
//
// All public methods are safe for concurrent use.
type UnifiedRegistry struct {
	mu            sync.RWMutex
	nodes         map[string]UnifiedNode
	order         []string
	resolvedCache [][]UnifiedNode
}

var defaultRegistry = NewRegistry()

// NewRegistry creates a new empty UnifiedRegistry.
func NewRegistry() *UnifiedRegistry {
	return &UnifiedRegistry{
		nodes: make(map[string]UnifiedNode),
	}
}

// DefaultRegistry returns the package-level singleton.
func DefaultRegistry() *UnifiedRegistry { return defaultRegistry }

// Add registers a node in the unified graph. Duplicate names overwrite.
func (r *UnifiedRegistry) Add(name string, kind NodeKind, runlevel dag.Runlevel) {
	r.mu.Lock()
	defer r.mu.Unlock()

	node := UnifiedNode{
		name:     name,
		kind:     kind,
		runlevel: runlevel,
		enabled:  true,
	}

	if _, exists := r.nodes[name]; !exists {
		r.order = append(r.order, name)
	}
	r.nodes[name] = node
	r.resolvedCache = nil
}

// Enable sets the enabled flag for the named node.
func (r *UnifiedRegistry) Enable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.setEnabledLocked(name, true)
}

// Disable clears the enabled flag for the named node.
func (r *UnifiedRegistry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.setEnabledLocked(name, false)
}

func (r *UnifiedRegistry) setEnabledLocked(name string, enabled bool) {
	if n, ok := r.nodes[name]; ok {
		n.enabled = enabled
		r.nodes[name] = n
		r.resolvedCache = nil
	}
}

// Reset clears all nodes and cached data, restoring the registry to its
// initial empty state.
func (r *UnifiedRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nodes = make(map[string]UnifiedNode)
	r.order = nil
	r.resolvedCache = nil
}

// LookupOrder returns the runlevel order for the named node, or false
// if the node is not registered.
func (r *UnifiedRegistry) LookupOrder(name string) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, ok := r.nodes[name]
	if !ok {
		return 0, false
	}

	return node.runlevel.Order, true
}

// InvalidateCache clears the resolved-batch cache. Per-type registries
// call this on mutation so the unified resolution stays consistent.
func (r *UnifiedRegistry) InvalidateCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.resolvedCache = nil
}

// ResolvedBatches returns nodes grouped by runlevel and topologically
// sorted within each batch. Only enabled nodes are included.
// Results are cached until the registry is mutated.
func (r *UnifiedRegistry) ResolvedBatches() ([][]UnifiedNode, error) {
	r.mu.RLock()
	if r.resolvedCache != nil {
		defer r.mu.RUnlock()
		return r.resolvedCache, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if r.resolvedCache != nil {
		return r.resolvedCache, nil
	}

	g := dag.NewGraph[UnifiedNode]()
	for _, name := range r.order {
		n := r.nodes[name]
		if !n.enabled {
			continue
		}
		g.Add(n)
	}

	batches, err := g.Resolve()
	if err != nil {
		return nil, err
	}

	r.resolvedCache = batches
	return batches, nil
}

// ReverseBatches returns batches in reverse order for cleanup.
func (r *UnifiedRegistry) ReverseBatches() ([][]UnifiedNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	g := dag.NewGraph[UnifiedNode]()
	for _, name := range r.order {
		n := r.nodes[name]
		if !n.enabled {
			continue
		}
		g.Add(n)
	}

	return g.ReverseBatches()
}

// ComponentsInBatch returns only the KindComponent entries from a batch.
func ComponentsInBatch(batch []UnifiedNode) []UnifiedNode {
	return filterBatch(batch, KindComponent)
}

// ModulesInBatch returns only the KindModule entries from a batch.
func ModulesInBatch(batch []UnifiedNode) []UnifiedNode {
	return filterBatch(batch, KindModule)
}

func filterBatch(batch []UnifiedNode, kind NodeKind) []UnifiedNode {
	var out []UnifiedNode
	for _, n := range batch {
		if n.kind == kind {
			out = append(out, n)
		}
	}
	return out
}

// Package-level convenience functions that delegate to the default registry.

func Add(name string, kind NodeKind, runlevel dag.Runlevel) {
	defaultRegistry.Add(name, kind, runlevel)
}

func Enable(name string) {
	defaultRegistry.Enable(name)
}

func Disable(name string) {
	defaultRegistry.Disable(name)
}

func InvalidateCache() {
	defaultRegistry.InvalidateCache()
}
