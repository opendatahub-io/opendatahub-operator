package fakeclient

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
)

func New(ctx context.Context, objs ...ctrlClient.Object) (*client.Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))

	fakeMapper := meta.NewDefaultRESTMapper(scheme.PreferredVersionAllGroups())
	for gvk := range scheme.AllKnownTypes() {
		fakeMapper.Add(gvk, meta.RESTScopeNamespace)
	}

	return client.New(
		ctx,
		nil,
		fake.NewClientBuilder().
			WithScheme(scheme).
			WithRESTMapper(fakeMapper).
			WithObjects(objs...).
			Build(),
	)
}
