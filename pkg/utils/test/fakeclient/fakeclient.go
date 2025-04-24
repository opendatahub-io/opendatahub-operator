package fakeclient

import (
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
)

func New(objs ...client.Object) (client.Client, error) {
	s, err := scheme.New()
	if err != nil {
		return nil, errors.New("unable to create default scheme")
	}

	for _, o := range objs {
		if err := resources.EnsureGroupVersionKind(s, o); err != nil {
			return nil, err
		}
	}

	fakeMapper := meta.NewDefaultRESTMapper(s.PreferredVersionAllGroups())
	for kt := range s.AllKnownTypes() {
		switch {
		case kt == gvk.CustomResourceDefinition:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case kt == gvk.ClusterRole:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		default:
			fakeMapper.Add(kt, meta.RESTScopeNamespace)
		}
	}

	ro := make([]runtime.Object, len(objs))
	for i := range objs {
		u, err := resources.ToUnstructured(objs[i])
		if err != nil {
			return nil, err
		}

		ro[i] = u
	}

	c := clientFake.NewClientBuilder().
		WithScheme(s).
		WithRESTMapper(fakeMapper).
		WithObjects(objs...).
		Build()

	return c, nil
}
