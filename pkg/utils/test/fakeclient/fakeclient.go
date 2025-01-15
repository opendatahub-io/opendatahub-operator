package fakeclient

import (
	oauthv1 "github.com/openshift/api/oauth/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	k8sFake "k8s.io/client-go/kubernetes/fake"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func New(objs ...ctrlClient.Object) (*client.Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(oauthv1.AddToScheme(scheme))
	utilruntime.Must(componentApi.AddToScheme(scheme))

	fakeMapper := meta.NewDefaultRESTMapper(scheme.PreferredVersionAllGroups())
	for gvk := range scheme.AllKnownTypes() {
		fakeMapper.Add(gvk, meta.RESTScopeNamespace)
	}

	ro := make([]runtime.Object, len(objs))
	for i := range objs {
		u, err := resources.ToUnstructured(objs[i])
		if err != nil {
			return nil, err
		}

		ro[i] = u
	}

	c := client.New(
		clientFake.NewClientBuilder().
			WithScheme(scheme).
			WithRESTMapper(fakeMapper).
			WithObjects(objs...).
			Build(),
		k8sFake.NewSimpleClientset(ro...),
		dynamicFake.NewSimpleDynamicClient(scheme, ro...),
	)

	return c, nil
}
