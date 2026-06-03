package mcplifecycleoperator

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &componentApi.MCPLifecycleOperator{}).
		// TODO: Add resource ownerships, watches, and actions
		Build(ctx)

	if err != nil {
		return err
	}

	return nil
}
