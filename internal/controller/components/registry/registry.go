package registry

import (
	"context"
	"sort"
	"sync"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

// ComponentHandler is an interface to manage a component
// Every method should accept ctx since it contains the logger.
type ComponentHandler interface {
	Init(platform common.Platform, cfg operatorconfig.OperatorSettings) error
	GetName() string
	// NewCRObject returns the component CR; if it returns an error, reconciliation fails
	// (e.g. Dashboard/ModelRegistry when gateway domain is unavailable).
	// Returning (nil, nil) is valid and indicates the component does not own a CR.
	// Callers must handle a nil return before dereferencing the result.
	NewCRObject(ctx context.Context, cli client.Client, dsc *dscv2.DataScienceCluster) (common.PlatformObject, error)
	NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error
	// UpdateDSCStatus updates the component specific status part of the DSC
	UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error)
	// IsEnabled returns whether the component should be deployed/is active
	IsEnabled(dsc *dscv2.DataScienceCluster) bool
}

// RegistrationOption configures optional orchestration metadata when adding
// a component to the registry.
type RegistrationOption func(*HandlerEntry)

// WithRunlevel sets the runlevel for DAG-based ordering. Lower runlevels
// are provisioned first; all nodes in a runlevel must be Ready before the
// next runlevel begins. Use the pre-defined constants in the dag package
// (e.g. dag.RL(20)). Components without an explicit runlevel default
// to dag.RL(99) (provisioned last).
func WithRunlevel(level dag.Runlevel) RegistrationOption {
	return func(e *HandlerEntry) {
		e.runlevel = level
	}
}

// HandlerEntry wraps a ComponentHandler with DAG ordering metadata.
type HandlerEntry struct {
	handler  ComponentHandler
	enabled  bool
	runlevel dag.Runlevel
}

func (e HandlerEntry) GetName() string              { return e.handler.GetName() }
func (e HandlerEntry) GetRunlevel() dag.Runlevel    { return e.runlevel }
func (e HandlerEntry) GetHandler() ComponentHandler { return e.handler } //nolint:ireturn

// Registry maintains a set of registered ComponentHandlers.
// All public methods are safe for concurrent use.
type Registry struct {
	mu            sync.RWMutex
	entries       map[string]HandlerEntry
	order         []string
	resolvedCache [][]HandlerEntry
}

var r = &Registry{}

// Add registers a new ComponentHandler to the registry.
func (r *Registry) Add(ch ComponentHandler, opts ...RegistrationOption) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		r.entries = make(map[string]HandlerEntry)
	}

	name := ch.GetName()

	e := HandlerEntry{handler: ch, enabled: true, runlevel: dag.RL(99)}
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

// Enable sets the enabled state for the named handler to true.
func (r *Registry) Enable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.setEnabledLocked(name, true)
}

// Disable sets the enabled state for the named handler to false.
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

// IsEnabled returns the internal enabled state for the named handler.
func (r *Registry) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[name]
	return ok && e.enabled
}

// sortedNames returns component names in sorted order for deterministic
// iteration. Caller must hold at least r.mu.RLock().
func (r *Registry) sortedNames() []string {
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ForEach iterates over all registered ComponentHandlers in DAG-resolved
// order and applies the given function. Handlers whose enabled flag is false
// are skipped. If DAG resolution fails, falls back to alphabetical order.
// If any handler returns an error, that error is collected and returned at the end.
func (r *Registry) ForEach(f func(ch ComponentHandler) error) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs *multierror.Error

	batches, resolveErr := r.resolvedBatchesLocked()
	if resolveErr != nil {
		ctrl.Log.WithName("component-registry").Error(resolveErr, "DAG resolution failed, falling back to alphabetical order")
		for _, name := range r.sortedNames() {
			e := r.entries[name]
			if !e.enabled {
				continue
			}
			errs = multierror.Append(errs, f(e.handler))
		}
		return errs.ErrorOrNil()
	}

	for _, batch := range batches {
		for _, e := range batch {
			errs = multierror.Append(errs, f(e.handler))
		}
	}

	return errs.ErrorOrNil()
}

// ForAll iterates over every registered component in alphabetical order
// regardless of enabled state. Use this for cleanup paths that must run
// even for suppressed components.
func (r *Registry) ForAll(f func(handler ComponentHandler, registryEnabled bool) error) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs *multierror.Error

	for _, name := range r.sortedNames() {
		e := r.entries[name]
		errs = multierror.Append(errs, f(e.handler, e.enabled))
	}

	return errs.ErrorOrNil()
}

// ResolvedBatches returns components grouped by runlevel and topologically
// sorted within each batch. Only enabled components are included.
// Results are cached until the registry is mutated.
func (r *Registry) ResolvedBatches() ([][]HandlerEntry, error) {
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

	return r.resolvedBatchesLocked()
}

// resolvedBatchesLocked resolves and caches batches. Caller must hold r.mu.
func (r *Registry) resolvedBatchesLocked() ([][]HandlerEntry, error) {
	if r.resolvedCache != nil {
		return r.resolvedCache, nil
	}

	g := dag.NewGraph[HandlerEntry]()
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
func (r *Registry) ReverseBatches() ([][]HandlerEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	g := dag.NewGraph[HandlerEntry]()
	for _, name := range r.order {
		e := r.entries[name]
		if !e.enabled {
			continue
		}
		g.Add(e)
	}

	return g.ReverseBatches()
}

// Lookup returns the handler for a named component, or nil if not found.
func (r *Registry) Lookup(name string) ComponentHandler { //nolint:ireturn
	r.mu.RLock()
	defer r.mu.RUnlock()

	if e, ok := r.entries[name]; ok {
		return e.handler
	}
	return nil
}

// AnyComponentEnabled returns true if at least one registered component is
// enabled in both the registry and the DataScienceCluster spec. Useful for
// skipping upgrade-gate checks when everything is Removed.
func (r *Registry) AnyComponentEnabled(dsc *dscv2.DataScienceCluster) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.enabled && e.handler.IsEnabled(dsc) {
			return true
		}
	}
	return false
}

// IsComponentEnabled checks if a component with the given name is enabled in the DataScienceCluster.
// Returns false if the component is not found or if it is disabled in the registry.
func (r *Registry) IsComponentEnabled(componentName string, dsc *dscv2.DataScienceCluster) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[componentName]
	return ok && e.enabled && e.handler.IsEnabled(dsc)
}

func Add(ch ComponentHandler, opts ...RegistrationOption) {
	r.Add(ch, opts...)
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

func ForEach(f func(ch ComponentHandler) error) error {
	return r.ForEach(f)
}

func DefaultRegistry() *Registry {
	return r
}

// IsComponentEnabled checks if a component with the given name is enabled in the DataScienceCluster
// using the default registry. Returns false if the component is not found.
func IsComponentEnabled(componentName string, dsc *dscv2.DataScienceCluster) bool {
	return r.IsComponentEnabled(componentName, dsc)
}
