package modules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
)

// checkLegacyMigrationReadiness waits for managed trackers to provision the
// target release and for removed modules to finish uninstalling.
func checkLegacyMigrationReadiness(
	ctx context.Context, cli client.Client, platform *configApi.Platform, release common.Release,
) error {
	return DefaultRegistry().ForAll(func(handler ModuleHandler, registryEnabled bool) error {
		name := handler.GetName()
		tracker, err := getPlatformModule(ctx, cli, name)
		if err != nil {
			return err
		}

		managed := registryEnabled && handler.IsEnabled(&platform.Spec.Modules)
		if managed {
			if tracker == nil || !PlatformOwnsResource(platform, tracker) || !tracker.DeletionTimestamp.IsZero() ||
				!trackerReleaseMatches(release, tracker.Status.Release) {
				return fmt.Errorf("waiting for module %s to publish a live tracker for release %s", name, release.Version.String())
			}
			provisioned := conditions.FindStatusCondition(tracker, status.ConditionTypeProvisioningSucceeded)
			if provisioned == nil || provisioned.Status != metav1.ConditionTrue || provisioned.ObservedGeneration != tracker.Generation {
				return fmt.Errorf("waiting for module %s to finish provisioning", name)
			}
			return nil
		}
		if tracker != nil && PlatformOwnsResource(platform, tracker) {
			return fmt.Errorf("waiting for removed module %s tracker to disappear", name)
		}

		// A module disabled on upgrade may never have had a tracker. Its
		// previous operator must stay available until the module CR is gone.
		state, err := handler.GetModuleCRState(ctx, cli)
		if err != nil {
			return fmt.Errorf("getting module %s CR state: %w", name, err)
		}
		if state != CRStateAbsent {
			return fmt.Errorf("waiting for removed module %s CR to disappear", name)
		}
		return nil
	})
}
