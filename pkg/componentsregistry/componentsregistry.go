// componentsregistry package is a registry of all components that can be managed by the operator
// TODO: it may make sense to put it under components/ when it's clear from the old stuff
package componentsregistry

import (
	"context"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// ComponentHandler is an interface to manage a component
// Every method should accept ctx since it contains the logger.
type ComponentHandler interface {
	Init(platform cluster.Platform) error
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
	UpdateDSCStatus(dsc *dscv1.DataScienceCluster, obj client.Object) error
}

var registry = []ComponentHandler{}

// Add registers a new component handler
// not thread safe, supposed to be called during init.
// TODO: check if init() can be called in parallel.
func Add(ch ComponentHandler) {
	registry = append(registry, ch)
}

// ForEach iterates over all registered component handlers
// With go1.23 probably https://go.dev/blog/range-functions can be used.
func ForEach(f func(ch ComponentHandler) error) error {
	var errs *multierror.Error
	for _, ch := range registry {
		errs = multierror.Append(errs, f(ch))
	}
	return errs.ErrorOrNil()
}

func IsManaged(ch ComponentHandler, dsc *dscv1.DataScienceCluster) bool {
	return ch.GetManagementState(dsc) == operatorv1.Managed
}
