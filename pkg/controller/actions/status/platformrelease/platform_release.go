package platformrelease

import (
	"context"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// NewPostStatusFn returns a PostStatusFn that stamps
// status.releases[name="platform"] with the current operator version
// when the component is happy and deploy was not skipped.
func NewPostStatusFn() reconciler.PostStatusFn {
	return func(ctx context.Context, rr *types.ReconciliationRequest, isHappy bool) error {
		if !isHappy || rr.SkipDeploy {
			return nil
		}

		obj, ok := rr.Instance.(common.WithReleases)
		if !ok {
			logf.FromContext(ctx).V(3).Info("Resource does not implement WithReleases, skipping platform release stamp")

			return nil
		}

		obj.GetReleaseStatus().SetPlatformRelease(rr.Release.Version.String())

		return nil
	}
}
