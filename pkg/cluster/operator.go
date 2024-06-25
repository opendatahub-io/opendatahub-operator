package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	v2 "github.com/operator-framework/api/pkg/operators/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSubscription checks if a Subscription for the operator exists in the given namespace.
// if exists, return object; otherwise, return error.
func GetSubscription(ctx context.Context, cli client.Client, namespace string, name string) (*v1alpha1.Subscription, error) {
	sub := &v1alpha1.Subscription{}
	if err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, sub); err != nil {
		// real error or 'not found' both return here
		return nil, err
	}
	return sub, nil
}

func SubscriptionExists(ctx context.Context, cli client.Client, name string) (bool, error) {
	subscriptionList := &v1alpha1.SubscriptionList{}
	if err := cli.List(ctx, subscriptionList); err != nil {
		return false, err
	}

	for _, sub := range subscriptionList.Items {
		if sub.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// DeleteExistingSubscription deletes given Subscription if it exists
// Do not error if the Subscription does not exist.
func DeleteExistingSubscription(ctx context.Context, cli client.Client, operatorNs string, subsName string) error {
	sub, err := GetSubscription(ctx, cli, operatorNs, subsName)
	if err != nil {
		return client.IgnoreNotFound(err)
	}

	if err := cli.Delete(ctx, sub); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("error deleting subscription %s: %w", sub.Name, err)
	}

	return nil
}

// OperatorExists checks if an Operator with 'operatorPrefix' is installed.
// Return true if found it, false if not.
// if we need to check exact version of the operator installed, can append vX.Y.Z later.
func OperatorExists(ctx context.Context, cli client.Client, operatorPrefix string) (bool, error) {
	opConditionList := &v2.OperatorConditionList{}
	err := cli.List(ctx, opConditionList)
	if err != nil {
		return false, err
	}
	for _, opCondition := range opConditionList.Items {
		if strings.HasPrefix(opCondition.Name, operatorPrefix) {
			return true, nil
		}
	}

	return false, nil
}
