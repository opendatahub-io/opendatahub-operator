// Package reconciler provides fixtures for recording reconciler API mutations.
package reconciler

import (
	"context"
	"slices"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// MutationRecorder records attempted writes while preserving the underlying client's behavior.
type MutationRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *MutationRecorder) record(operation string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, operation)
}

// Calls returns a snapshot of attempted writes in call order.
func (r *MutationRecorder) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// Reset clears recorded calls between reconciliation attempts.
func (r *MutationRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

// Interceptors records every normal and subresource mutation before delegating it.
func (r *MutationRecorder) Interceptors() interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			r.record("create")
			return cli.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			r.record("update")
			return cli.Update(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			r.record("patch")
			return cli.Patch(ctx, obj, patch, opts...)
		},
		Apply: func(ctx context.Context, cli client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			r.record("apply")
			return cli.Apply(ctx, obj, opts...)
		},
		Delete: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			r.record("delete")
			return cli.Delete(ctx, obj, opts...)
		},
		DeleteAllOf: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
			r.record("deleteAllOf")
			return cli.DeleteAllOf(ctx, obj, opts...)
		},
		SubResourceCreate: func(ctx context.Context, cli client.Client, name string, obj, sub client.Object, opts ...client.SubResourceCreateOption) error {
			r.record(name + "/create")
			return cli.SubResource(name).Create(ctx, obj, sub, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, cli client.Client, name string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			r.record(name + "/update")
			return cli.SubResource(name).Update(ctx, obj, opts...)
		},
		SubResourcePatch: func(ctx context.Context, cli client.Client, name string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			r.record(name + "/patch")
			return cli.SubResource(name).Patch(ctx, obj, patch, opts...)
		},
		SubResourceApply: func(ctx context.Context, cli client.Client, name string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
			r.record(name + "/apply")
			return cli.SubResource(name).Apply(ctx, obj, opts...)
		},
	}
}
