package servicemeshoperatorv2

import (
	"context"
	"fmt"
	"slices"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const subscriptionName = "servicemeshoperatorv2"

var blockingChannels = []string{
	"stable",
	"v2.x",
}

func Check(ctx context.Context, cli client.Reader, _ string, _ string) error {
	subscriptions := &operatorsv1alpha1.SubscriptionList{}
	if err := cli.List(ctx, subscriptions); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("listing Service Mesh subscriptions: %w", err)
	}

	for i := range subscriptions.Items {
		sub := &subscriptions.Items[i]
		if sub.Name != subscriptionName || !slices.Contains(blockingChannels, sub.Spec.Channel) {
			continue
		}

		return &UpgradeBlockedError{
			SubscriptionNamespace: sub.Namespace,
			SubscriptionName:      sub.Name,
			Channel:               sub.Spec.Channel,
			InstalledCSV:          sub.Status.InstalledCSV,
		}
	}

	return nil
}
