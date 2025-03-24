package fixtures

import (
	"context"

	"github.com/onsi/gomega"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func CreateSubscription(ctx context.Context, client client.Client, namespace, subscriptionYaml string) error {
	subscription := &ofapiv1alpha1.Subscription{}
	if err := yaml.Unmarshal([]byte(subscriptionYaml), subscription); err != nil {
		return err
	}

	ns := NewNamespace(namespace)
	if err := CreateOrUpdateNamespace(ctx, client, ns); err != nil {
		return err
	}
	return createOrUpdateSubscription(ctx, client, subscription)
}

func CreateOrUpdateNamespace(ctx context.Context, client client.Client, ns *corev1.Namespace) error {
	_, err := controllerutil.CreateOrUpdate(ctx, client, ns, func() error {
		return nil
	})
	return err
}

func createOrUpdateSubscription(ctx context.Context, client client.Client, subscription *ofapiv1alpha1.Subscription) error {
	_, err := controllerutil.CreateOrUpdate(ctx, client, subscription, func() error {
		return nil
	})
	return err
}

func CreateOrUpdateDSCI(ctx context.Context, client client.Client, dsci *dsciv1.DSCInitialization) error {
	_, err := controllerutil.CreateOrUpdate(ctx, client, dsci, func() error {
		return nil
	})
	dsci.APIVersion = dsciv1.GroupVersion.String()
	dsci.Kind = gvk.DSCInitialization.Kind
	return err
}

func NewNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func GetNamespace(ctx context.Context, client client.Client, namespace string) (*corev1.Namespace, error) {
	ns := NewNamespace(namespace)
	err := client.Get(ctx, types.NamespacedName{Name: namespace}, ns)

	return ns, err
}

func GetService(ctx context.Context, client client.Client, namespace, name string) (*corev1.Service, error) {
	svc := &corev1.Service{}
	err := client.Get(ctx, types.NamespacedName{
		Name: name, Namespace: namespace,
	}, svc)

	return svc, err
}

func CreateService(ctx context.Context, client client.Client, namespace, svcName string) (*corev1.Service, error) {
	if err := client.Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"name": "istio-operator",
			},
			Ports: []corev1.ServicePort{
				{
					Port: 443,
				},
			},
		},
	}); err != nil {
		return nil, err
	}
	return GetService(ctx, client, namespace, svcName)
}

func CreateSecret(name, namespace string) feature.Action {
	return func(ctx context.Context, cli client.Client, f *feature.Feature) error {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					f.AsOwnerReference(),
				},
			},
			Data: map[string][]byte{
				"test": []byte("test"),
			},
		}

		return cli.Create(ctx, secret)
	}
}

func GetFeatureTracker(ctx context.Context, cli client.Client, appNamespace, featureName string) (*featurev1.FeatureTracker, error) {
	tracker := featurev1.NewFeatureTracker(featureName, appNamespace)
	err := cli.Get(ctx, client.ObjectKey{
		Name: tracker.Name,
	}, tracker)

	return tracker, err
}

func NewDSCInitialization(ctx context.Context, cli client.Client, dsciName, ns string) *dsciv1.DSCInitialization {
	dsci := &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.DSCInitialization.Version,
			Kind:       gvk.DSCInitialization.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dsciName,
		},
	}

	_, errCreate := controllerutil.CreateOrUpdate(ctx, cli, dsci, func() error {
		dsci.Spec = dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: ns,
			ServiceMesh: &infrav1.ServiceMeshSpec{
				ManagementState: "Managed",
				ControlPlane: infrav1.ControlPlaneSpec{
					Name:              "data-science-smcp",
					Namespace:         "istio-system",
					MetricsCollection: "Istio",
				},
			},
		}

		return nil
	})

	gomega.Expect(errCreate).ToNot(gomega.HaveOccurred())

	return dsci
}
