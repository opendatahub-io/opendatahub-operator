package modules

import (
	"context"
	"fmt"
	"strings"

	semver "github.com/blang/semver/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// aggregatePlatformModulesStatus includes pending removal as well as managed
// tracker readiness. A disabled module is finished only when its tracker is gone.
func aggregatePlatformModulesStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	platform, ok := rr.Instance.(*configApi.Platform)
	if !ok {
		return fmt.Errorf("aggregatePlatformModulesStatus requires Platform instance, got %T", rr.Instance)
	}
	var notReady []string
	managed := 0
	preflightErrors := ModuleGatePreflightErrors(rr)
	err := DefaultRegistry().ForAll(func(handler ModuleHandler, registryEnabled bool) error {
		enabled := registryEnabled && handler.IsEnabled(&platform.Spec.Modules)
		name := handler.GetName()
		if enabled {
			managed++
		}
		tracker, err := getPlatformModule(ctx, rr.Client, name)
		if err != nil {
			return err
		}
		if tracker == nil {
			if enabled {
				message := name
				if failure := preflightErrors[name]; failure != "" {
					message += ": " + failure
				}
				notReady = append(notReady, message)
			}
			return nil
		}
		ready := conditions.FindStatusCondition(tracker, status.ConditionTypeReady)
		if !enabled {
			if platformOwnsTracker(platform, tracker) {
				message := name + ": removal pending"
				if ready != nil && ready.Message != "" {
					message += " (" + ready.Message + ")"
				}
				notReady = append(notReady, message)
			}
			return nil
		}
		if message := preflightErrors[name]; message != "" {
			notReady = append(notReady, name+": "+message)
		} else if !tracker.DeletionTimestamp.IsZero() || ready == nil || ready.Status != metav1.ConditionTrue {
			notReady = append(notReady, name)
		} else if !trackerReleaseMatches(rr.Release, tracker.Status.Release) {
			notReady = append(notReady, fmt.Sprintf("%s: upgrade pending (%s %s -> %s %s)", name,
				tracker.Status.Release.Name, tracker.Status.Release.Version.String(), rr.Release.Name, rr.Release.Version.String()))
		}
		return nil
	})
	if err != nil {
		return err
	}
	switch {
	case len(notReady) > 0:
		rr.Conditions.SetCondition(common.Condition{
			Type: status.ConditionTypeModulesReady, Status: metav1.ConditionFalse,
			Reason: status.NotReadyReason, Message: fmt.Sprintf("Modules not ready: %s", strings.Join(notReady, ", ")),
		})
	case managed == 0:
		rr.Conditions.SetCondition(common.Condition{
			Type: status.ConditionTypeModulesReady, Status: metav1.ConditionTrue, Severity: common.ConditionSeverityInfo,
			Reason: status.NoManagedModulesReason, Message: "No managed modules",
		})
	default:
		rr.Conditions.MarkTrue(status.ConditionTypeModulesReady)
	}
	return nil
}

func trackerReleaseMatches(target, tracker common.Release) bool {
	return !tracker.Version.Equals(semver.Version{}) &&
		tracker.Version.String() == target.Version.String() && tracker.Name == target.Name
}
