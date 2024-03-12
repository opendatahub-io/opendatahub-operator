package fixtures

import (
	"context"

	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func CreateSubscription(subscriptionYaml, namespace string, client client.Client) error {
	subscription := &ofapiv1alpha1.Subscription{}
	if err := yaml.Unmarshal([]byte(subscriptionYaml), subscription); err != nil {
		return err
	}

	ns := NewNamespace(namespace)
	if err := createOrUpdateNamespace(client, ns); err != nil {
		return err
	}
	return createOrUpdateSubscription(client, subscription)
}

func createOrUpdateNamespace(client client.Client, ns *v1.Namespace) error {
	_, err := controllerutil.CreateOrUpdate(context.Background(), client, ns, func() error {
		return nil
	})
	return err
}

func createOrUpdateSubscription(client client.Client, subscription *ofapiv1alpha1.Subscription) error {
	_, err := controllerutil.CreateOrUpdate(context.Background(), client, subscription, func() error {
		return nil
	})
	return err
}

func NewNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}
