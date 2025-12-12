package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OperatorInfo Struct to retrieve the information about an installed operator.
type OperatorInfo struct {
	Version string
}

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
// If the operator exists, it returns some operator information.
// If the operator does not exist, it returns a nil reference.
func OperatorExists(ctx context.Context, cli client.Client, operatorPrefix string) (*OperatorInfo, error) {
	opConditionList := &ofapiv2.OperatorConditionList{}
	err := cli.List(ctx, opConditionList)
	if err != nil {
		// return nil reference and the error when parsing the list
		return nil, err
	}
	for _, opCondition := range opConditionList.Items {
		expectedPrefix := fmt.Sprintf("%s.", operatorPrefix)
		if !strings.HasPrefix(opCondition.Name, expectedPrefix) {
			// Skip if no OperatorCondition is found with the expected prefix
			continue
		}
		// Get the version from the operatorCondition name, trimming the prefix.
		version := strings.TrimPrefix(opCondition.Name, expectedPrefix)
		if version == "" {
			// Return Operator info with an empty version if the version is empty.
			return &OperatorInfo{Version: ""}, nil
		}
		// Return the OperatorInfo
		if !strings.HasPrefix(version, "v") {
			version = fmt.Sprintf("v%s", version)
		}
		return &OperatorInfo{Version: version}, nil
	}
	// return nil reference if the operator is not installed in the cluster
	return nil, nil
}

// CustomResourceDefinitionExists checks if a CustomResourceDefinition with the given GVK exists.
func CustomResourceDefinitionExists(ctx context.Context, cli client.Client, crdGK schema.GroupKind) error {
	crd := &apiextv1.CustomResourceDefinition{}
	name := strings.ToLower(fmt.Sprintf("%ss.%s", crdGK.Kind, crdGK.Group)) // we need plural form of the kind

	backoff := wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   1.0,
		Steps:    5,
	}
	// 5 second timeout
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, client.ObjectKey{Name: name}, crd)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextv1.Established {
				if condition.Status == apiextv1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	})

	return err
}
