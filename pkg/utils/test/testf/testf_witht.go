package testf

import (
	"context"
	"time"

	"github.com/onsi/gomega"
	gomegaTypes "github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// WithT encapsulates the test context and the Kubernetes client, along with gomega's assertion methods.
// It provides utility methods to interact with resources in a Kubernetes cluster and perform assertions on them.
type WithT struct {
	ctx    context.Context
	client client.Client

	*gomega.WithT
}

// WithTOpts is a function type used to configure options for the WithT object.
// These options modify the behavior of the tests, such as timeouts and polling intervals.
type WithTOpts func(*WithT)

func WithFailHandler(value gomegaTypes.GomegaFailHandler) WithTOpts {
	return func(g *WithT) {
		g.WithT = g.ConfigureWithFailHandler(value)
	}
}

func WithEventuallyTimeout(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultEventuallyTimeout(value)
	}
}

func WithEventuallyPollingInterval(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultEventuallyPollingInterval(value)
	}
}

func WithConsistentlyDuration(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultConsistentlyDuration(value)
	}
}

func WithConsistentlyPollingInterval(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultConsistentlyPollingInterval(value)
	}
}

func (t *WithT) Context() context.Context {
	return t.ctx
}

// Client returns the `client.Client` used to interact with the cluster for resource operations.
//
// Returns:
//   - client.Client: The Kubernetes client used for performing operations on the cluster.
func (t *WithT) Client() client.Client {
	return t.client
}

func (t *WithT) List(
	gvk schema.GroupVersionKind,
	option ...client.ListOption,
) *EventuallyValue[[]unstructured.Unstructured] {
	return &EventuallyValue[[]unstructured.Unstructured]{
		ctx: t.Context(),
		g:   t.WithT,
		f: func(ctx context.Context) ([]unstructured.Unstructured, error) {
			items := unstructured.UnstructuredList{}
			items.SetGroupVersionKind(gvk)

			err := t.Client().List(ctx, &items, option...)
			if err != nil {
				return nil, StopErr(err, "failed to list resource: %s", gvk)
			}

			return items.Items, nil
		},
	}
}

func (t *WithT) Get(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	option ...client.GetOption,
) *EventuallyValue[*unstructured.Unstructured] {
	return &EventuallyValue[*unstructured.Unstructured]{
		ctx: t.Context(),
		g:   t.WithT,
		f: func(ctx context.Context) (*unstructured.Unstructured, error) {
			u := unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)

			err := t.Client().Get(ctx, nn, &u, option...)
			switch {
			case errors.IsNotFound(err):
				return nil, nil
			case err != nil:
				return nil, StopErr(err, "failed to get resource: %s, nn: %s", gvk, nn.String())
			default:
				return &u, nil
			}
		},
	}
}

func (t *WithT) Delete(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	option ...client.DeleteOption,
) *EventuallyErr {
	return &EventuallyErr{
		ctx: t.Context(),
		g:   t.WithT,
		f: func(ctx context.Context) error {
			u := resources.GvkToUnstructured(gvk)
			u.SetName(nn.Name)
			u.SetNamespace(nn.Namespace)

			err := t.Client().Delete(ctx, u, option...)
			switch {
			case errors.IsNotFound(err):
				return nil
			case err != nil:
				return StopErr(err, "failed to delete resource: %s, nn: %s", gvk, nn.String())
			default:
				return nil
			}
		},
	}
}

func (t *WithT) Update(
	gvk schema.GroupVersionKind,
	nn types.NamespacedName,
	fn func(obj *unstructured.Unstructured) error,
	option ...client.UpdateOption,
) *EventuallyValue[*unstructured.Unstructured] {
	return &EventuallyValue[*unstructured.Unstructured]{
		ctx: t.Context(),
		g:   t.WithT,
		f: func(ctx context.Context) (*unstructured.Unstructured, error) {
			obj := resources.GvkToUnstructured(gvk)

			err := t.Client().Get(ctx, nn, obj)
			switch {
			case errors.IsNotFound(err):
				return nil, nil
			case err != nil:
				return nil, StopErr(err, "failed to get resource: %s, nn: %s", gvk, nn.String())
			}

			in, err := resources.ToUnstructured(obj)
			if err != nil {
				return nil, StopErr(err, "failed to convert to unstructured")
			}

			if err := fn(in); err != nil {
				return nil, StopErr(err, "failed to apply function")
			}

			err = t.Client().Update(ctx, in, option...)
			switch {
			case errors.IsForbidden(err):
				return nil, StopErr(err, "failed to update resource: %s, nn: %s", gvk, nn.String())
			case err != nil:
				return nil, err
			default:
				return in, nil
			}
		},
	}
}
