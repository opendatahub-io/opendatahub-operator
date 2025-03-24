// componentsregistry package is a registry of all components that can be managed by the operator
// TODO: it may make sense to put it under components/ when it's clear from the old stuff
package componentsregistry

import (
	"context"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// ComponentHandler is an interface to manage a component
// Every method should accept ctx since it contains the logger.
type ComponentHandler interface {
	Init(platform common.Platform) error
	// GetName and GetManagementState sound like pretty much the same across
	// all components, but I could not find a way to avoid it
	GetName() string
	GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState
	// NewCRObject constructs components specific Custom Resource
	// e.g. Dashboard in datasciencecluster.opendatahub.io group
	// It returns interface, but it simplifies DSC reconciler code a lot
	NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject
	NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error
	// UpdateDSCStatus updates the component specific status part of the DSC
	UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error)
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

func Add(ch ComponentHandler) {
	r.Add(ch)
}

func ForEach(f func(ch ComponentHandler) error) error {
	return r.ForEach(f)
}

func DefaultRegistry() *Registry {
	return r
}

func IsManaged(ch ComponentHandler, dsc *dscv1.DataScienceCluster) bool {
	return ch.GetManagementState(dsc) == operatorv1.Managed
}
