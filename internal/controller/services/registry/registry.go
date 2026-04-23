package registry

import (
	"context"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
)

// ServiceHandler is an interface to manage a service
// Every method should accept ctx since it contains the logger.
type ServiceHandler interface {
	Init(platform common.Platform) error
	GetName() string
	GetManagementState(platform common.Platform, dsci *dsciv2.DSCInitialization) operatorv1.ManagementState
	NewReconciler(ctx context.Context, mgr ctrl.Manager) error
}

type handlerEntry struct {
	handler ServiceHandler
	enabled bool
}

// Registry is a struct that maintains a set of registered ServiceHandlers.
type Registry struct {
	entries map[string]handlerEntry
}

var r = &Registry{}

// Add registers a new ServiceHandler to the registry.
// not thread safe, supposed to be called during program initialization.
func (r *Registry) Add(ch ServiceHandler) {
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

// ForEach iterates over all registered ServiceHandlers and applies the given function.
// Handlers whose enabled flag is false are skipped.
// If any handler returns an error, that error is collected and returned at the end.
// With go1.23 probably https://go.dev/blog/range-functions can be used.
func (r *Registry) ForEach(f func(ch ServiceHandler) error) error {
	var errs *multierror.Error
	for _, e := range r.entries {
		if !e.enabled {
			continue
		}
		errs = multierror.Append(errs, f(e.handler))
	}

	return errs.ErrorOrNil()
}

func Add(ch ServiceHandler) {
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

func ForEach(f func(ch ServiceHandler) error) error {
	return r.ForEach(f)
}

func DefaultRegistry() *Registry {
	return r
}
