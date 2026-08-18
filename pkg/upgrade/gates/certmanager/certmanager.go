package certmanager

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	subscriptionName      = "openshift-cert-manager-operator"
	subscriptionNamespace = "cert-manager-operator"
)

func Check(ctx context.Context, cli client.Reader, _ string, _ string) error {
	found, err := cluster.HasSubscription(ctx, cli, subscriptionNamespace, subscriptionName)
	if err != nil {
		return fmt.Errorf("checking cert-manager subscription: %w", err)
	}
	if found {
		return nil
	}

	return &UpgradeBlockedError{
		SubscriptionNamespace: subscriptionNamespace,
		SubscriptionName:      subscriptionName,
	}
}
