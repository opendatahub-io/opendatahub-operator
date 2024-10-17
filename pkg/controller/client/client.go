package client

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
)

func NewFromManager(ctx context.Context, mgr ctrl.Manager) (*Client, error) {
	return New(ctx, mgr.GetConfig(), mgr.GetClient())
}

func New(_ context.Context, cfg *rest.Config, client ctrlCli.Client) (*Client, error) {
	k8sCli, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to construct a playground client: %w", err)
	}

	return &Client{
		Client: client,
		K:      k8sCli,
	}, nil
}

type Client struct {
	ctrlCli.Client
	K kubernetes.Interface
}

func (c *Client) Apply(ctx context.Context, obj ctrlCli.Object, opts ...ctrlCli.PatchOption) error {
	// remove not required fields
	obj.SetManagedFields(nil)

	err := c.Client.Patch(ctx, obj, ctrlCli.Apply, opts...)
	if err != nil {
		return fmt.Errorf("unable to pactch object %s: %w", obj, err)
	}

	return nil
}

func (c *Client) ApplyStatus(ctx context.Context, obj ctrlCli.Object, opts ...ctrlCli.SubResourcePatchOption) error {
	// remove not required fields
	obj.SetManagedFields(nil)

	err := c.Client.Status().Patch(ctx, obj, ctrlCli.Apply, opts...)
	if err != nil {
		return fmt.Errorf("unable to pactch object status %s: %w", obj, err)
	}

	return nil
}
