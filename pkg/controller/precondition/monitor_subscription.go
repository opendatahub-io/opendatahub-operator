package precondition

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type SubscriptionDependency struct {
	// Name is the OLM subscription name to check for presence.
	Name string

	// DisplayName is the human-readable name used in condition messages.
	DisplayName string
}

// MonitorSubscriptions creates a precondition that checks for OLM subscription
// presence. All subscriptions must be present for the check to pass. Missing
// subscriptions are reported with their display names.
// Defaults to OpenShift-only (OLM subscriptions are an OpenShift concept).
// Pass WithClusterTypes to override.
// Panics if subscriptions is empty or any entry has empty Name or DisplayName.
func MonitorSubscriptions(subscriptions []SubscriptionDependency, opts ...Option) PreCondition {
	if len(subscriptions) == 0 {
		panic("MonitorSubscriptions called with empty subscription list")
	}

	for _, sub := range subscriptions {
		if sub.Name == "" || sub.DisplayName == "" {
			panic(fmt.Sprintf("SubscriptionDependency has empty Name or DisplayName: %+v", sub))
		}
	}

	// Prevent caller mutations from affecting the check closure.
	subs := slices.Clone(subscriptions)

	pc := newPreCondition(func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		var missing []string

		for _, sub := range subs {
			found, err := cluster.SubscriptionExists(ctx, rr.Client, sub.Name)
			if err != nil {
				return CheckResult{}, fmt.Errorf("failed to check %s subscription: %w", sub.DisplayName, err)
			}

			if !found {
				missing = append(missing, sub.DisplayName+": subscription not found")
			}
		}

		if len(missing) == 0 {
			return CheckResult{Pass: true}, nil
		}

		return CheckResult{
			Pass:    false,
			Message: strings.Join(missing, "; "),
		}, nil
	}, append([]Option{WithClusterTypes(cluster.ClusterTypeOpenShift)}, opts...)...)

	return pc
}
