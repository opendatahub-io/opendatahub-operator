package kueueoperator

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	kueuegates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kueue"
)

const subscriptionName = "kueue-operator"

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	state, err := kueuegates.ManagementState(ctx, reader)
	if err != nil {
		return err
	}

	switch state {
	case "":
		return nil
	case string(operatorv1.Managed):
		return &UpgradeBlockedError{ManagedStateUnsupported: true}
	case string(operatorv1.Unmanaged):
		installed, err := cluster.SubscriptionExists(ctx, reader, subscriptionName)
		if err != nil {
			return err
		}
		if !installed {
			return &UpgradeBlockedError{MissingKueueOperatorSubscription: true}
		}
	}

	return nil
}
