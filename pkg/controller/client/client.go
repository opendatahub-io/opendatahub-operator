package client

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func NewFromManager(ctx context.Context, mgr ctrl.Manager) (*Client, error) {
	return New(ctx, mgr.GetConfig(), mgr.GetClient())
}

func New(_ context.Context, _ *rest.Config, client ctrlCli.Client) (*Client, error) {
	return &Client{
		Client: client,
	}, nil
}

type Client struct {
	ctrlCli.Client
}

func (c *Client) Apply(ctx context.Context, in ctrlCli.Object, opts ...ctrlCli.PatchOption) error {
	u, err := resources.ToUnstructured(in)
	if err != nil {
		return fmt.Errorf("failed to convert resource to unstructured: %w", err)
	}

	// safe copy
	u = u.DeepCopy()

	// remove not required fields
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(u.Object, "status")

	err = c.Client.Patch(ctx, u, ctrlCli.Apply, opts...)
	if err != nil {
		return fmt.Errorf("unable to pactch object %s: %w", u, err)
	}

	// Write back the modified object so callers can access the patched object.
	err = c.Scheme().Convert(u, in, ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to write modified object")
	}

	return nil
}

func (c *Client) ApplyStatus(ctx context.Context, in ctrlCli.Object, opts ...ctrlCli.SubResourcePatchOption) error {
	u, err := resources.ToUnstructured(in)
	if err != nil {
		return fmt.Errorf("failed to convert resource to unstructured: %w", err)
	}

	// safe copy
	u = u.DeepCopy()

	// remove not required fields
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")

	err = c.Client.Status().Patch(ctx, u, ctrlCli.Apply, opts...)
	if err != nil {
		return fmt.Errorf("unable to patch object status %s: %w", u, err)
	}

	// Write back the modified object so callers can access the patched object.
	err = c.Scheme().Convert(u, in, ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to write modified object")
	}

	return nil
}
