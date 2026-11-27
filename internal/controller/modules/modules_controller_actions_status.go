package modules

import (
	"context"
	"fmt"
	"strings"

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
				return nil
			}
			// The tracker is gone but the module CR may still be terminating:
			// its operator must stay available until the CR's finalizers run.
			// Report removal as pending instead of dropping the module from
			// the aggregate; "No managed modules" only holds once the CR is
			// absent too.
			crState, crErr := handler.GetModuleCRState(ctx, rr.Client)
			if crErr != nil {
				// An unconfirmed removal state must not hide the failure.
				return fmt.Errorf("getting %s module CR state: %w", name, crErr)
			}
			if crState != CRStateAbsent {
				notReady = append(notReady, name+": removal pending")
			}
			return nil
		}
		ready := conditions.FindStatusCondition(tracker, status.ConditionTypeReady)
		if !enabled {
			if PlatformOwnsResource(platform, tracker) {
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
		} else if !PlatformOwnsResource(platform, tracker) || !tracker.DeletionTimestamp.IsZero() || ready == nil || ready.Status != metav1.ConditionTrue {
			// A same-name tracker owned by another Platform instance (e.g.
			// left over after the Platform CR was recreated) must not count
			// as ready for this Platform.
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
	return tracker.Version.String() == target.Version.String() && tracker.Name == target.Name
}
