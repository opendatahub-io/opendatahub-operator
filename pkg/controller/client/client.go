package client

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func NewFromManager(mgr ctrl.Manager) (*Client, error) {
	return NewFromConfig(mgr.GetConfig(), mgr.GetClient())
}

func NewFromConfig(cfg *rest.Config, client ctrlCli.Client) (*Client, error) {
	kubernetesCl, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to construct a Kubernetes client: %w", err)
	}

	dynamicCl, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to construct a Discovery client: %w", err)
	}

	return New(client, kubernetesCl, dynamicCl), nil
}

func New(client ctrlCli.Client, kubernetes kubernetes.Interface, dynamic dynamic.Interface) *Client {
	return &Client{
		Client:     client,
		kubernetes: kubernetes,
		dynamic:    dynamic,
	}
}

type Client struct {
	ctrlCli.Client
	kubernetes kubernetes.Interface
	dynamic    dynamic.Interface
}

func (c *Client) Kubernetes() kubernetes.Interface {
	return c.kubernetes
}

func (c *Client) Discovery() discovery.DiscoveryInterface {
	return c.kubernetes.Discovery()
}

func (c *Client) Dynamic() dynamic.Interface {
	return c.dynamic
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
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("unable to patch object %s: %w", u, err)
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
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("unable to patch object status %s: %w", u, err)
	}

	// Write back the modified object so callers can access the patched object.
	err = c.Scheme().Convert(u, in, ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to write modified object")
	}

	return nil
}
