package servicemeshoperatorv2

import (
	"context"
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const subscriptionPackage = "servicemeshoperator"

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
		if sub.Spec == nil || sub.Spec.Package != subscriptionPackage {
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
