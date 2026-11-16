package modules

import (
	"sort"
	"sync"

	"github.com/hashicorp/go-multierror"
)

type registryEntry struct {
	handler      ModuleHandler
	enabled      bool
	configSource ConfigSource
}

// Registry maintains the set of registered ModuleHandlers.
// All public methods are safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]registryEntry
}

// Reset removes all registered handlers. Intended for isolated controller
// tests that exercise the process-wide default registry.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = nil
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

	e := registryEntry{handler: handler, enabled: true}
	for _, opt := range opts {
		opt(&e)
	}

	r.entries[name] = e
}

// Disable sets the enabled state for the named module to false.
func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.entries[name]; ok {
		e.enabled = false
		r.entries[name] = e
	}
}

// IsEnabled returns the internal enabled state for the named module.
func (r *Registry) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[name]
	return ok && e.enabled
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

// ForConfigSource iterates over modules whose config source matches,
// regardless of enabled state.
func (r *Registry) ForConfigSource(source ConfigSource, f func(handler ModuleHandler, registryEnabled bool) error) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs *multierror.Error
	for _, name := range r.sortedNames() {
		e := r.entries[name]
		if e.configSource != source {
			continue
		}
		errs = multierror.Append(errs, f(e.handler, e.enabled))
	}

	return errs.ErrorOrNil()
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

// Package-level functions that delegate to the default registry.

func Add(handler ModuleHandler, opts ...RegistrationOption) {
	r.Add(handler, opts...)
}

// Disable marks the module as disabled via env var at operator startup.
func Disable(name string) {
	r.Disable(name)
}

func IsEnabled(name string) bool {
	return r.IsEnabled(name)
}

func ForConfigSource(source ConfigSource, f func(handler ModuleHandler, registryEnabled bool) error) error {
	return r.ForConfigSource(source, f)
}

func DefaultRegistry() *Registry {
	return r
}
