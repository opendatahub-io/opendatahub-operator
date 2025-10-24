package fakeclient

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
)

type clientOptions struct {
	scheme      *runtime.Scheme
	interceptor interceptor.Funcs
	objects     []client.Object
}
type ClientOpts func(*clientOptions)

func WithInterceptorFuncs(value interceptor.Funcs) ClientOpts {
	return func(o *clientOptions) {
		o.interceptor = value
	}
}

func WithObjects(values ...client.Object) ClientOpts {
	return func(o *clientOptions) {
		o.objects = append(o.objects, values...)
	}
}

func WithScheme(value *runtime.Scheme) ClientOpts {
	return func(o *clientOptions) {
		o.scheme = value
	}
}

func New(opts ...ClientOpts) (client.Client, error) {
	co := clientOptions{}
	for _, o := range opts {
		o(&co)
	}

	s := co.scheme
	if s == nil {
		newScheme, err := scheme.New()
		if err != nil {
			return nil, fmt.Errorf("unable to create default scheme: %w", err)
		}

		s = newScheme
	}

	for _, o := range co.objects {
		if err := resources.EnsureGroupVersionKind(s, o); err != nil {
			return nil, err
		}
	}

	fakeMapper := meta.NewDefaultRESTMapper(s.PreferredVersionAllGroups())
	for kt := range s.AllKnownTypes() {
		switch kt {
		// k8s
		case gvk.CustomResourceDefinition:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.ClusterRole:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		// ODH
		case gvk.DataScienceCluster:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.DSCInitialization:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.Auth:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.DashboardHardwareProfile:
			fakeMapper.Add(kt, meta.RESTScopeNamespace)
		default:
			fakeMapper.Add(kt, meta.RESTScopeNamespace)
		}
	}

	b := clientFake.NewClientBuilder()
	b = b.WithScheme(s)
	b = b.WithRESTMapper(fakeMapper)
	b = b.WithObjects(co.objects...)
	b = b.WithInterceptorFuncs(co.interceptor)

	return b.Build(), nil
}
