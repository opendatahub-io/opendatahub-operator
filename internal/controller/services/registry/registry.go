package registry

import (
	"context"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// ServiceHandler is an interface to manage a service
// Every method should accept ctx since it contains the logger.
type ServiceHandler interface {
	Init(platform common.Platform) error
	GetName() string
	GetManagementState(platform common.Platform) operatorv1.ManagementState
	NewReconciler(ctx context.Context, mgr ctrl.Manager) error
}

// Registry is a struct that maintains a list of registered ServiceHandlers.
type Registry struct {
	handlers []ServiceHandler
}

var r = &Registry{}

// Add registers a new ServiceHandler to the registry.
// not thread safe, supposed to be called during init.
func (r *Registry) Add(ch ServiceHandler) {
	r.handlers = append(r.handlers, ch)
}

// ForEach iterates over all registered ServiceHandlers and applies the given function.
// If any handler returns an error, that error is collected and returned at the end.
// With go1.23 probably https://go.dev/blog/range-functions can be used.
func (r *Registry) ForEach(f func(ch ServiceHandler) error) error {
	var errs *multierror.Error
	for _, ch := range r.handlers {
		errs = multierror.Append(errs, f(ch))
	}

	return errs.ErrorOrNil()
}

func Add(ch ServiceHandler) {
	r.Add(ch)
}

func ForEach(f func(ch ServiceHandler) error) error {
	return r.ForEach(f)
}

func DefaultRegistry() *Registry {
	return r
}
