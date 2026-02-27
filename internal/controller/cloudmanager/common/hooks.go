package common

import (
	"context"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// CreateNamespaceHook returns a hook that ensures a namespace exists.
func CreateNamespaceHook(namespace string) types.HookFn {
	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		_, err := cluster.CreateNamespace(ctx, rr.Client, namespace)
		return err
	}
}
