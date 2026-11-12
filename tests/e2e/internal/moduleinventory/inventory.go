package moduleinventory

import (
	"fmt"
	"slices"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// Module contains the production registration metadata needed by the E2E
// module contract runner.
type Module struct {
	Name         string
	Handler      modules.ModuleHandler
	Runlevel     dag.Runlevel
	ConfigSource modules.ConfigSource
}

// Inventory is an immutable, registration-ordered view of production modules.
type Inventory struct {
	modules []Module
	byName  map[string]Module
}

// Builtins builds an inventory from the production built-in module registry.
func Builtins() (Inventory, error) {
	return New(builtin.Registrations())
}

// New validates registrations and returns an inventory that preserves their
// production provisioning order.
func New(registrations []builtin.ModuleRegistration) (Inventory, error) {
	inventory := Inventory{
		modules: make([]Module, 0, len(registrations)),
		byName:  make(map[string]Module, len(registrations)),
	}

	for index, registration := range registrations {
		if registration.Handler == nil {
			return Inventory{}, fmt.Errorf("module registration %d has no handler", index)
		}

		name := registration.Handler.GetName()
		if name == "" {
			return Inventory{}, fmt.Errorf("module registration %d has an empty name", index)
		}
		if _, found := inventory.byName[name]; found {
			return Inventory{}, fmt.Errorf("duplicate module registration %q", name)
		}

		module := Module{
			Name:         name,
			Handler:      registration.Handler,
			Runlevel:     registration.Runlevel,
			ConfigSource: registration.ConfigSource,
		}
		inventory.modules = append(inventory.modules, module)
		inventory.byName[name] = module
	}

	return inventory, nil
}

// All returns every module in production registration order.
func (i Inventory) All() []Module {
	return slices.Clone(i.modules)
}

// Names returns every module name in production registration order.
func (i Inventory) Names() []string {
	names := make([]string, 0, len(i.modules))
	for _, module := range i.modules {
		names = append(names, module.Name)
	}
	return names
}

// Lookup returns the module with the given production registration name.
func (i Inventory) Lookup(name string) (Module, bool) {
	module, found := i.byName[name]
	return module, found
}

// Select resolves module names strictly and returns the selected modules in
// production registration order. An empty selection returns every module.
func (i Inventory) Select(names ...string) ([]Module, error) {
	if len(names) == 0 {
		return i.All(), nil
	}

	requested := make(map[string]struct{}, len(names))
	var unknown []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, found := i.byName[name]; !found {
			unknown = append(unknown, name)
			continue
		}
		requested[name] = struct{}{}
	}

	if len(unknown) > 0 {
		slices.Sort(unknown)
		unknown = slices.Compact(unknown)
		return nil, fmt.Errorf("unknown module(s) %s; valid modules are: %s", strings.Join(unknown, ", "), strings.Join(i.Names(), ", "))
	}

	selected := make([]Module, 0, len(requested))
	for _, module := range i.modules {
		if _, found := requested[module.Name]; found {
			selected = append(selected, module)
		}
	}
	return selected, nil
}

// ForConfigSource returns modules owned by the requested configuration source
// in production registration order.
func (i Inventory) ForConfigSource(source modules.ConfigSource) []Module {
	selected := make([]Module, 0, len(i.modules))
	for _, module := range i.modules {
		if module.ConfigSource == source {
			selected = append(selected, module)
		}
	}
	return selected
}
