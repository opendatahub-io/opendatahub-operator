package releases

import (
	"context"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const PlatformReleaseName = "platform"

// NewPlatformVersionAction returns an action that records the current
// operator version as a "platform" entry in status.releases. This
// mirrors the module version handshake pattern: the operator writes
// the platform version after a successful reconcile, and the DAG
// readiness checker verifies it matches before advancing runlevels.
//
// When SkipDeploy is true (runlevel gate not cleared), the action is
// a no-op so the version is not recorded until the component has
// actually reconciled at the current platform version.
//
// The action requires that the resource instance implements the
// common.WithReleases interface. Components that do not implement it
// are silently skipped with a log message.
func NewPlatformVersionAction() actions.Fn {
	return recordPlatformVersion
}

func recordPlatformVersion(ctx context.Context, rr *types.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	obj, ok := rr.Instance.(common.WithReleases)
	if !ok {
		logf.FromContext(ctx).V(3).Info(
			"Resource does not implement WithReleases, skipping platform version recording",
		)
		return nil
	}

	version := rr.Release.Version.String()
	releases := obj.GetReleaseStatus()

	var updated []common.ComponentRelease
	if releases != nil {
		updated = make([]common.ComponentRelease, 0, len(*releases)+1)
		updated = append(updated, *releases...)
	}

	// Upsert the platform release entry.
	found := false
	for i := range updated {
		if updated[i].Name == PlatformReleaseName {
			updated[i].Version = version
			found = true
			break
		}
	}

	if !found {
		updated = append(updated, common.ComponentRelease{
			Name:    PlatformReleaseName,
			Version: version,
		})
	}

	obj.SetReleaseStatus(updated)

	return nil
}
