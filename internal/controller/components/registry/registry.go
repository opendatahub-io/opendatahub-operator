package registry

import (
	"context"
	"sort"
	"sync"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

// ComponentHandler is an interface to manage a component
// Every method should accept ctx since it contains the logger.
type ComponentHandler interface {
	Init(platform common.Platform, cfg operatorconfig.OperatorSettings) error
	GetName() string
	GroupVersionKind() schema.GroupVersionKind
	// NewCRObject returns the component CR; if it returns an error, reconciliation fails
	// (e.g. Dashboard/AIHub when gateway domain is unavailable).
	// Returning (nil, nil) is valid and indicates the component does not own a CR.
	// Callers must handle a nil return before dereferencing the result.
	NewCRObject(ctx context.Context, cli client.Client, dsc *dscApi.DataScienceCluster) (common.PlatformObject, error)
	NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error
	// UpdateDSCStatus updates the component specific status part of the DSC
	UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error)
	// IsEnabled returns whether the component should be deployed/is active
	IsEnabled(dsc *dscApi.DataScienceCluster) bool
}

type registryEntry struct {
	handler ComponentHandler
	enabled bool
}

// Registry maintains a set of registered ComponentHandlers.
// Ordering and readiness gating belong to the unified provisioning registry.
// All public methods are safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]registryEntry
}

var r = &Registry{}

// Add registers a new ComponentHandler to the registry.
func (r *Registry) Add(ch ComponentHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		r.entries = make(map[string]registryEntry)
	}
	r.entries[ch.GetName()] = registryEntry{
		handler: ch,
		enabled: true,
	}
}

// Disable suppresses the named component at operator startup.
func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.entries[name]; ok {
		e.enabled = false
		r.entries[name] = e
	}
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

// ForEach visits enabled handlers alphabetically and collects callback errors.
func (r *Registry) ForEach(f func(ch ComponentHandler) error) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs *multierror.Error
	for _, name := range r.sortedNames() {
		e := r.entries[name]
		if e.enabled {
			errs = multierror.Append(errs, f(e.handler))
		}
	}
	return errs.ErrorOrNil()
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

func Add(ch ComponentHandler) {
	r.Add(ch)
}

// Disable marks the component as disabled via env var at operator startup.
func Disable(name string) {
	r.Disable(name)
}

func ForEach(f func(ch ComponentHandler) error) error {
	return r.ForEach(f)
}

func DefaultRegistry() *Registry {
	return r
}
