package modules

import (
	"sort"
	"sync"

	"github.com/hashicorp/go-multierror"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
)

type registryEntry struct {
	handler  ModuleHandler
	enabled  bool
	runlevel dag.Runlevel
}

func (e registryEntry) GetName() string           { return e.handler.GetName() }
func (e registryEntry) GetRunlevel() dag.Runlevel { return e.runlevel }

// Registry maintains the set of registered ModuleHandlers.
// All public methods are safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]registryEntry
	// order preserves insertion order for deterministic iteration.
	order []string
	// resolvedCache caches the DAG-resolved batches; invalidated on mutation.
	resolvedCache [][]registryEntry
}

var r = &Registry{}

// Add registers a new ModuleHandler to the registry.
func (r *Registry) Add(handler ModuleHandler, opts ...RegistrationOption) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		r.entries = make(map[string]registryEntry)
	}

	name := handler.GetName()

	e := registryEntry{handler: handler, enabled: true, runlevel: dag.RL(99)}
	for _, opt := range opts {
		opt(&e)
	}

	if _, exists := r.entries[name]; !exists {
		r.order = append(r.order, name)
	}
	r.entries[name] = e
	r.resolvedCache = nil
	provision.InvalidateCache()
}

// Enable sets the enabled state for the named module to true.
func (r *Registry) Enable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.setEnabledLocked(name, true)
}

// Disable sets the enabled state for the named module to false.
func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.setEnabledLocked(name, false)
}

// setEnabledLocked sets the enabled state. Caller must hold r.mu.
func (r *Registry) setEnabledLocked(name string, enabled bool) {
	if e, ok := r.entries[name]; ok {
		e.enabled = enabled
		r.entries[name] = e
		r.resolvedCache = nil
		provision.InvalidateCache()
	}
}

// IsEnabled returns the internal enabled state for the named module.
func (r *Registry) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[name]
	return ok && e.enabled
}

// EnableFromList enables only the named modules, disabling all others.
// Names that don't match any registered module are silently ignored.
func (r *Registry) EnableFromList(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for name, e := range r.entries {
		e.enabled = want[name]
		r.entries[name] = e
	}
	r.resolvedCache = nil
	provision.InvalidateCache()
}

// sortedNames returns module names in sorted order for deterministic iteration.
// Caller must hold at least r.mu.RLock().
func (r *Registry) sortedNames() []string {
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ForEach iterates over all registered modules in sorted order and applies
// the given function. Entries whose enabled flag is false are skipped.
// Errors are collected and returned at the end.
func (r *Registry) ForEach(f func(ModuleHandler) error) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs *multierror.Error
	for _, name := range r.sortedNames() {
		e := r.entries[name]
		if !e.enabled {
			continue
		}
		errs = multierror.Append(errs, f(e.handler))
	}

	return errs.ErrorOrNil()
}

// ForEachEnabled iterates over enabled modules in sorted order and invokes
// the given function. Unlike ForEach, the callback has no return value,
// making it suitable when callers collect errors through an external
// mechanism (e.g. a failedModules slice).
func (r *Registry) ForEachEnabled(f func(ModuleHandler)) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range r.sortedNames() {
		e := r.entries[name]
		if !e.enabled {
			continue
		}
		f(e.handler)
	}
}

// ForAll iterates over every registered module in sorted order regardless of
// enabled state. Use this for cleanup paths that must run even for suppressed
// modules.
func (r *Registry) ForAll(f func(handler ModuleHandler, registryEnabled bool) error) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs *multierror.Error
	for _, name := range r.sortedNames() {
		e := r.entries[name]
		errs = multierror.Append(errs, f(e.handler, e.enabled))
	}

	return errs.ErrorOrNil()
}

// IsModuleEnabled checks if a module with the given name is enabled in the
// registry and also enabled based on platform configuration.
func (r *Registry) IsModuleEnabled(moduleName string, platform *PlatformContext) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[moduleName]
	return ok && e.enabled && e.handler.IsEnabled(platform)
}

// ResolvedBatches returns modules grouped by runlevel and topologically
// sorted within each batch. Only enabled modules are included.
// Results are cached until the registry is mutated.
func (r *Registry) ResolvedBatches() ([][]registryEntry, error) {
	r.mu.RLock()
	if r.resolvedCache != nil {
		defer r.mu.RUnlock()
		return r.resolvedCache, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.resolvedCache != nil {
		return r.resolvedCache, nil
	}

	g := dag.NewGraph[registryEntry]()
	for _, name := range r.order {
		e := r.entries[name]
		if !e.enabled {
			continue
		}
		g.Add(e)
	}

	batches, err := g.Resolve()
	if err != nil {
		return nil, err
	}
	r.resolvedCache = batches
	return batches, nil
}

// ReverseBatches returns batches in reverse order for cleanup.
func (r *Registry) ReverseBatches() ([][]registryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	g := dag.NewGraph[registryEntry]()
	for _, name := range r.order {
		e := r.entries[name]
		if !e.enabled {
			continue
		}
		g.Add(e)
	}

	return g.ReverseBatches()
}

// Lookup returns the handler for a named module, or nil if not found.
func (r *Registry) Lookup(name string) ModuleHandler { //nolint:ireturn
	r.mu.RLock()
	defer r.mu.RUnlock()

	if e, ok := r.entries[name]; ok {
		return e.handler
	}
	return nil
}

// HasEntries returns true if there are any registered modules.
func (r *Registry) HasEntries() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.entries) > 0
}

// AnyEnabled returns true if at least one registered module is enabled
// in the given PlatformContext. Returns false when all modules are
// Removed or no modules are registered.
func (r *Registry) AnyEnabled(platform *PlatformContext) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.handler.IsEnabled(platform) {
			return true
		}
	}
	return false
}

// Package-level functions that delegate to the default registry.

func Add(handler ModuleHandler, opts ...RegistrationOption) {
	r.Add(handler, opts...)
}

func Enable(name string) {
	r.Enable(name)
}

func Disable(name string) {
	r.Disable(name)
}

func IsEnabled(name string) bool {
	return r.IsEnabled(name)
}

func EnableFromList(names []string) {
	r.EnableFromList(names)
}

func ForEach(f func(ModuleHandler) error) error {
	return r.ForEach(f)
}

func ForEachEnabled(f func(ModuleHandler)) {
	r.ForEachEnabled(f)
}

func ForAll(f func(handler ModuleHandler, registryEnabled bool) error) error {
	return r.ForAll(f)
}

func IsModuleEnabled(moduleName string, platform *PlatformContext) bool {
	return r.IsModuleEnabled(moduleName, platform)
}

func DefaultRegistry() *Registry {
	return r
}
