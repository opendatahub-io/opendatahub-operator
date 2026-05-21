package modules

import (
	"sort"

	"github.com/hashicorp/go-multierror"
)

type registryEntry struct {
	handler      ModuleHandler
	enabled      bool
	runlevel     int
	dependencies []string
}

// Registry maintains the set of registered ModuleHandlers.
type Registry struct {
	entries map[string]registryEntry
	// order preserves insertion order for deterministic iteration.
	order []string
}

var r = &Registry{}

// Add registers a new ModuleHandler to the registry.
// Not thread safe; call during program initialization only.
func (r *Registry) Add(handler ModuleHandler, opts ...RegistrationOption) {
	if r.entries == nil {
		r.entries = make(map[string]registryEntry)
	}

	name := handler.GetName()

	e := registryEntry{handler: handler, enabled: true}
	for _, opt := range opts {
		opt(&e)
	}

	if _, exists := r.entries[name]; !exists {
		r.order = append(r.order, name)
	}
	r.entries[name] = e
}

// Enable sets the enabled state for the named module to true.
func (r *Registry) Enable(name string) {
	r.setEnabled(name, true)
}

// Disable sets the enabled state for the named module to false.
func (r *Registry) Disable(name string) {
	r.setEnabled(name, false)
}

func (r *Registry) setEnabled(name string, enabled bool) {
	if e, ok := r.entries[name]; ok {
		e.enabled = enabled
		r.entries[name] = e
	}
}

// IsEnabled returns the internal enabled state for the named module.
func (r *Registry) IsEnabled(name string) bool {
	e, ok := r.entries[name]
	return ok && e.enabled
}

// EnableFromList enables only the named modules, disabling all others.
// Names that don't match any registered module are silently ignored.
func (r *Registry) EnableFromList(names []string) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for name, e := range r.entries {
		e.enabled = want[name]
		r.entries[name] = e
	}
}

// sortedNames returns module names in sorted order for deterministic iteration.
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

// ForAll iterates over every registered module in sorted order regardless of
// enabled state. Use this for cleanup paths that must run even for suppressed
// modules.
func (r *Registry) ForAll(f func(handler ModuleHandler, registryEnabled bool) error) error {
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
	e, ok := r.entries[moduleName]
	return ok && e.enabled && e.handler.IsEnabled(platform)
}

// HasEntries returns true if there are any registered modules.
func (r *Registry) HasEntries() bool {
	return len(r.entries) > 0
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

func ForAll(f func(handler ModuleHandler, registryEnabled bool) error) error {
	return r.ForAll(f)
}

func IsModuleEnabled(moduleName string, platform *PlatformContext) bool {
	return r.IsModuleEnabled(moduleName, platform)
}

func DefaultRegistry() *Registry {
	return r
}
