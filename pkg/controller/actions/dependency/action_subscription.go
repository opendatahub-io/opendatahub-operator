package dependency

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// SubscriptionDependency identifies a single OLM subscription to check.
type SubscriptionDependency struct {
	// Name is the OLM subscription name to look for (e.g. "rhcl-operator").
	Name string

	// DisplayName is the human-readable name for error messages (e.g. "Red Hat Connectivity Link").
	DisplayName string
}

// SubscriptionGroupConfig defines a group of subscriptions that map to a single condition.
// All subscriptions in the group must be present for the condition to be True.
type SubscriptionGroupConfig struct {
	// ConditionType is the condition to set based on subscription presence.
	ConditionType string

	// Subscriptions is the list of subscriptions ALL of which must be present
	// for the group to be satisfied.
	Subscriptions []SubscriptionDependency

	// ClusterTypes restricts this group to run only on specific cluster types
	// (e.g. cluster.ClusterTypeOpenShift). If empty, the check runs on all
	// cluster types.
	ClusterTypes []string

	// Reason is the reason string when subscriptions are missing.
	Reason string

	// Message is a fmt template receiving %s with the joined missing display names.
	Message string

	// Severity determines the condition severity when subscriptions are missing.
	// Use ConditionSeverityInfo for optional dependencies (informational only).
	// Default: ConditionSeverityError
	Severity common.ConditionSeverity
}

// SubscriptionAction checks OLM subscriptions and sets per-group conditions on the component status.
type SubscriptionAction struct {
	groups []SubscriptionGroupConfig
}

// SubscriptionActionOpts is a functional option for configuring the SubscriptionAction.
type SubscriptionActionOpts func(*SubscriptionAction)

// CheckSubscriptionGroup adds a subscription group to check.
// Each group maps to a separate condition on the component status.
func CheckSubscriptionGroup(config SubscriptionGroupConfig) SubscriptionActionOpts {
	return func(a *SubscriptionAction) {
		if config.Severity == "" {
			config.Severity = common.ConditionSeverityError
		}
		a.groups = append(a.groups, config)
	}
}

// NewSubscriptionAction creates an action that checks for OLM subscriptions
// and sets per-group conditions on the component status.
//
// Unlike NewAction which aggregates all checks into a single DependenciesAvailable
// condition, each subscription group produces its own independent condition,
// allowing fine-grained feature-level dependency tracking.
func NewSubscriptionAction(opts ...SubscriptionActionOpts) actions.Fn {
	action := SubscriptionAction{}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

func (a *SubscriptionAction) run(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	clusterType := cluster.GetClusterInfo().Type

	for i := range a.groups {
		group := &a.groups[i]

		if len(group.ClusterTypes) > 0 && !slices.Contains(group.ClusterTypes, clusterType) {
			// the condition is not set if the clusterer type does not match.
			continue
		}

		rr.Conditions.MarkUnknown(group.ConditionType)

		var missing []string
		for _, sub := range group.Subscriptions {
			found, err := cluster.SubscriptionExists(ctx, rr.Client, sub.Name)
			if err != nil {
				return fmt.Errorf("failed to check %s subscription: %w", sub.DisplayName, err)
			}
			if !found {
				missing = append(missing, sub.DisplayName)
			}
		}

		if len(missing) == 0 {
			rr.Conditions.MarkTrue(group.ConditionType)
		} else {
			rr.Conditions.MarkFalse(
				group.ConditionType,
				cond.WithSeverity(group.Severity),
				cond.WithReason(group.Reason),
				cond.WithMessage(group.Message, strings.Join(missing, " and ")),
			)
		}
	}

	return nil
}
