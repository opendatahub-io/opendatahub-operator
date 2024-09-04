package safeclient

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SafeClient struct {
	cli  client.Client
	lock sync.Mutex
}

func New(cli client.Client) *SafeClient {
	return &SafeClient{
		cli: cli,
	}
}

// Reader.
func (c *SafeClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.Get(ctx, key, obj, opts...)
}

func (c *SafeClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.List(ctx, list, opts...)
}

// Writer.
func (c *SafeClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.Create(ctx, obj, opts...)
}
func (c *SafeClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.Delete(ctx, obj, opts...)
}
func (c *SafeClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.Update(ctx, obj, opts...)
}
func (c *SafeClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.Patch(ctx, obj, patch, opts...)
}
func (c *SafeClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.DeleteAllOf(ctx, obj, opts...)
}

// StatusClient.
func (c *SafeClient) Status() client.SubResourceWriter { //nolint:ireturn
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.Status()
}

// SubResourceClientConstructor.
func (c *SafeClient) SubResource(subResource string) client.SubResourceClient { //nolint:ireturn
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.SubResource(subResource)
}

// Own methods.
func (c *SafeClient) Scheme() *runtime.Scheme {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.Scheme()
}
func (c *SafeClient) RESTMapper() meta.RESTMapper { //nolint:ireturn
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.RESTMapper()
}
func (c *SafeClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.GroupVersionKindFor(obj)
}
func (c *SafeClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cli.IsObjectNamespaced(obj)
}
