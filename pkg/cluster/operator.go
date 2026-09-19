package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
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
			if k8serr.IsNotFound(err) {
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
