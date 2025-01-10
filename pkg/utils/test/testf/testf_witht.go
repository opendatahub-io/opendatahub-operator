package testf

import (
	"context"
	"time"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type WithTOpts func(*WithT)

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

type WithT struct {
	ctx    context.Context
	client *odhClient.Client

	*gomega.WithT
}

func (t *WithT) Context() context.Context {
	return t.ctx
}

func (t *WithT) Client() *odhClient.Client {
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
