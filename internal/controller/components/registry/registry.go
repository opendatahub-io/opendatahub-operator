package registry

import (
	"context"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// ComponentHandler is an interface to manage a component
// Every method should accept ctx since it contains the logger.
type ComponentHandler interface {
	Init(platform common.Platform) error
	GetName() string
	// NewCRObject constructs components specific Custom Resource
	// e.g. Dashboard in datasciencecluster.opendatahub.io group
	// It returns interface, but it simplifies DSC reconciler code a lot
	NewCRObject(dsc *dscv2.DataScienceCluster) common.PlatformObject
	NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error
	// UpdateDSCStatus updates the component specific status part of the DSC
	UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error)
	// IsEnabled returns whether the component should be deployed/is active
	IsEnabled(dsc *dscv2.DataScienceCluster) bool
}

// Registry is a struct that maintains a list of registered ComponentHandlers.
type Registry struct {
	handlers []ComponentHandler
}

var r = &Registry{}

// Add registers a new ComponentHandler to the registry.
// not thread safe, supposed to be called during init.
func (r *Registry) Add(ch ComponentHandler) {
	r.handlers = append(r.handlers, ch)
}

// ForEach iterates over all registered ComponentHandlers and applies the given function.
// If any handler returns an error, that error is collected and returned at the end.
// With go1.23 probably https://go.dev/blog/range-functions can be used.
func (r *Registry) ForEach(f func(ch ComponentHandler) error) error {
	var errs *multierror.Error
	for _, ch := range r.handlers {
		errs = multierror.Append(errs, f(ch))
	}

	return errs.ErrorOrNil()
}

// IsComponentEnabled checks if a component with the given name is enabled in the DataScienceCluster.
// Returns false if the component is not found.
func (r *Registry) IsComponentEnabled(componentName string, dsc *dscv2.DataScienceCluster) bool {
	for _, ch := range r.handlers {
		if ch.GetName() == componentName {
			return ch.IsEnabled(dsc)
		}
	}
	return false
}

func Add(ch ComponentHandler) {
	r.Add(ch)
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
