package registry

import (
	"context"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

// ComponentHandler is an interface to manage a component
// Every method should accept ctx since it contains the logger.
type ComponentHandler interface {
	Init(platform common.Platform, cfg operatorconfig.OperatorSettings) error
	GetName() string
	// NewCRObject returns the component CR; if it returns an error, reconciliation fails (e.g. Dashboard/ModelRegistry when gateway domain is unavailable).
	NewCRObject(ctx context.Context, cli client.Client, dsc *dscv2.DataScienceCluster) (common.PlatformObject, error)
	NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error
	// UpdateDSCStatus updates the component specific status part of the DSC
	UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error)
	// IsEnabled returns whether the component should be deployed/is active
	IsEnabled(dsc *dscv2.DataScienceCluster) bool
}

type handlerEntry struct {
	handler ComponentHandler
	enabled bool
}

// Registry is a struct that maintains a set of registered ComponentHandlers.
type Registry struct {
	entries map[string]handlerEntry
}

var r = &Registry{}

// Add registers a new ComponentHandler to the registry.
// not thread safe, supposed to be called during program initialization.
func (r *Registry) Add(ch ComponentHandler) {
	if r.entries == nil {
		r.entries = make(map[string]handlerEntry)
	}
	r.entries[ch.GetName()] = handlerEntry{handler: ch, enabled: true}
}

// Enable sets the enabled state for the named handler to true.
func (r *Registry) Enable(name string) {
	r.setEnabled(name, true)
}

// Disable sets the enabled state for the named handler to false.
func (r *Registry) Disable(name string) {
	r.setEnabled(name, false)
}

// setEnabled sets the enabled state for the named handler.
func (r *Registry) setEnabled(name string, enabled bool) {
	if e, ok := r.entries[name]; ok {
		e.enabled = enabled
		r.entries[name] = e
	}
}

// IsEnabled returns the internal enabled state for the named handler.
func (r *Registry) IsEnabled(name string) bool {
	e, ok := r.entries[name]
	return ok && e.enabled
}

// ForEach iterates over all registered ComponentHandlers and applies the given function.
// Handlers whose enabled flag is false are skipped.
// If any handler returns an error, that error is collected and returned at the end.
// With go1.23 probably https://go.dev/blog/range-functions can be used.
func (r *Registry) ForEach(f func(ch ComponentHandler) error) error {
	var errs *multierror.Error
	for _, e := range r.entries {
		if !e.enabled {
			continue
		}
		errs = multierror.Append(errs, f(e.handler))
	}

	return errs.ErrorOrNil()
}

// IsComponentEnabled checks if a component with the given name is enabled in the DataScienceCluster.
// Returns false if the component is not found or if it is disabled in the registry.
func (r *Registry) IsComponentEnabled(componentName string, dsc *dscv2.DataScienceCluster) bool {
	e, ok := r.entries[componentName]
	return ok && e.enabled && e.handler.IsEnabled(dsc)
}

func Add(ch ComponentHandler) {
	r.Add(ch)
}

func Enable(name string) {
	r.setEnabled(name, true)
}

func Disable(name string) {
	r.setEnabled(name, false)
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
