package security

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func NewUpdatePodSecurityRoleBindingAction(roles map[cluster.Platform][]string) actions.Fn {
	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		v := roles[rr.Release.Name]
		if len(v) == 0 {
			return nil
		}

		err := cluster.UpdatePodSecurityRolebinding(ctx, rr.Client, rr.DSCI.Spec.ApplicationsNamespace, v...)
		if err != nil {
			return fmt.Errorf("failed to update PodSecurityRolebinding for %s: %w", v, err)
		}

		return nil
	}
}
