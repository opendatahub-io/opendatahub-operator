package sanitycheck

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type UnwantedResource struct {
	GVK          schema.GroupVersionKind
	ErrorMessage string
}

type Action struct {
	unwantedResources []UnwantedResource
}

type ActionOpts func(*Action)

// NewAction creates a sanity check action performing some checks to verify cluster
// is in a correct state before proceeding with component reconciliation.
// This is typically used to ensure that certain deprecated or conflicting resources
// are not present before proceeding with component deployment or upgrade operations.
//
// It verifies:
//   - specified unwanted resources do not exist in the cluster
func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

func WithUnwantedResource(gvk schema.GroupVersionKind, errorMessage string) ActionOpts {
	return func(action *Action) {
		action.unwantedResources = append(action.unwantedResources, UnwantedResource{GVK: gvk, ErrorMessage: errorMessage})
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	for _, unwantedResource := range a.unwantedResources {
		if err := a.ensureResourceNotExists(ctx, rr, unwantedResource); err != nil {
			return err
		}
	}

	return nil
}

// ensureResourceNotExists checks if the specified resource exists in the cluster.
// If CRD is not present, it returns nil.
// If it does, it returns an error with the specified error message.
// If it does not, it returns nil.
func (a *Action) ensureResourceNotExists(ctx context.Context, rr *types.ReconciliationRequest, unwantedResource UnwantedResource) error {
	gvk := unwantedResource.GVK

	hasCrd, err := cluster.HasCRD(ctx, rr.Client, gvk)
	if err != nil {
		return fmt.Errorf("failed to check if %s CRD exists: %w", gvk, err)
	}

	if !hasCrd {
		return nil
	}

	resources, err := cluster.ListGVK(ctx, rr.Client, gvk)
	if err != nil {
		return fmt.Errorf("failed to list %s resources: %w", gvk, err)
	}

	// If we found any resources, sanity check failed
	if len(resources) > 0 {
		return odherrors.NewStopError("%s", unwantedResource.ErrorMessage)
	}

	return nil
}
