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

		setPlatformRelease(obj, rr.Release.Version.String())

		return nil
	}
}

func setPlatformRelease(wr common.WithReleases, version string) {
	releases := wr.GetReleaseStatus()
	for i, r := range *releases {
		if r.Name == common.PlatformReleaseName {
			(*releases)[i].Version = version

			if i != 0 {
				entry := (*releases)[i]
				*releases = append([]common.ComponentRelease{entry}, append((*releases)[:i], (*releases)[i+1:]...)...)
			}
			return
		}
	}

	*releases = append([]common.ComponentRelease{{
		Name:    common.PlatformReleaseName,
		Version: version,
	}}, *releases...)
}
