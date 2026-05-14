package fakeclient

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
)

// GVKMapping associates a GVK with a REST scope for registration in the fake
// client's REST mapper. Use this for types that have no Go struct in the scheme
// (e.g. kueue resources used as unstructured).
type GVKMapping struct {
	GVK   schema.GroupVersionKind
	Scope meta.RESTScope
}

type clientOptions struct {
	scheme      *runtime.Scheme
	interceptor interceptor.Funcs
	objects     []client.Object
	gvkMappings []GVKMapping
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

func WithGVKs(mappings ...GVKMapping) ClientOpts {
	return func(o *clientOptions) {
		o.gvkMappings = append(o.gvkMappings, mappings...)
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
		// Gateway API (cluster-scoped)
		case gvk.GatewayClass:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		// ODH
		case gvk.DataScienceCluster:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.DSCInitialization:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.DataScienceClusterV1:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.DSCInitializationV1:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.Auth:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.GatewayConfig:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		default:
			fakeMapper.Add(kt, meta.RESTScopeNamespace)
		}
	}

	for _, m := range co.gvkMappings {
		fakeMapper.Add(m.GVK, m.Scope)
	}

	b := clientFake.NewClientBuilder()
	b = b.WithScheme(s)
	b = b.WithRESTMapper(fakeMapper)
	b = b.WithObjects(co.objects...)
	b = b.WithInterceptorFuncs(co.interceptor)

	return b.Build(), nil
}
