package e2e_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const moduleCleanupHoldFinalizer = "test.opendatahub.io/data-module-cleanup-hold"

// Capture the identity read by the successful update retry, including when the
// response is lost, so cleanup never removes a hold from a replacement module.
func addModuleCleanupFinalizer(ctx context.Context, cli client.Client, obj *unstructured.Unstructured) (types.UID, error) {
	var heldUID types.UID
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := cli.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		heldUID = obj.GetUID()
		controllerutil.AddFinalizer(obj, moduleCleanupHoldFinalizer)
		return cli.Update(ctx, obj)
	})
	return heldUID, err
}

func releaseModuleCleanupFinalizer(ctx context.Context, cli client.Client, obj *unstructured.Unstructured, uid types.UID) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := obj.DeepCopy()
		if err := cli.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
			return client.IgnoreNotFound(err)
		}
		if fresh.GetUID() != uid || !controllerutil.RemoveFinalizer(fresh, moduleCleanupHoldFinalizer) {
			return nil
		}
		return cli.Update(ctx, fresh)
	})
}

func newModuleCleanupTestObject(uid types.UID) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(dataModuleCRGVK)
	obj.SetName(dataModuleCRName)
	obj.SetUID(uid)
	obj.SetFinalizers([]string{"platform.opendatahub.io/finalizer"})
	return obj
}

func TestModuleCleanupFinalizerRelease(t *testing.T) {
	const hold = moduleCleanupHoldFinalizer
	for _, tc := range []struct {
		name   string
		uid    types.UID
		absent bool
	}{
		{name: "original module", uid: "original"},
		{name: "replacement module", uid: "replacement"},
		{name: "already deleted", absent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := newModuleCleanupTestObject(tc.uid)
			obj.SetFinalizers(append(obj.GetFinalizers(), hold))
			builder := fake.NewClientBuilder()
			if !tc.absent {
				builder = builder.WithObjects(obj)
			}
			cli := builder.Build()
			require.NoError(t, releaseModuleCleanupFinalizer(t.Context(), cli, obj, "original"))
			err := cli.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)
			if tc.absent {
				require.True(t, k8serr.IsNotFound(err))
				return
			}
			require.NoError(t, err)
			require.Contains(t, obj.GetFinalizers(), "platform.opendatahub.io/finalizer")
			if tc.uid == "original" {
				require.NotContains(t, obj.GetFinalizers(), hold)
			} else {
				require.Contains(t, obj.GetFinalizers(), hold)
			}
		})
	}
}

func TestModuleCleanupFinalizerCapturesCurrentIdentity(t *testing.T) {
	const hold = moduleCleanupHoldFinalizer
	t.Run("replacement before get", func(t *testing.T) {
		obj := newModuleCleanupTestObject("stale")
		current := newModuleCleanupTestObject("current")
		cli := fake.NewClientBuilder().WithObjects(current).Build()
		uid, err := addModuleCleanupFinalizer(t.Context(), cli, obj)
		require.NoError(t, err)
		require.Equal(t, types.UID("current"), uid)
		require.NoError(t, releaseModuleCleanupFinalizer(t.Context(), cli, obj, uid))
		require.NoError(t, cli.Get(t.Context(), client.ObjectKeyFromObject(obj), current))
		require.NotContains(t, current.GetFinalizers(), hold)
	})
	t.Run("replacement during conflict retry", func(t *testing.T) {
		obj := newModuleCleanupTestObject("original")
		first := true
		cli := fake.NewClientBuilder().WithObjects(obj.DeepCopy()).WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, cli client.WithWatch, current client.Object, opts ...client.UpdateOption) error {
				if first {
					first = false
					fresh := obj.DeepCopy()
					fresh.SetUID("replacement")
					fresh.SetFinalizers([]string{"platform.opendatahub.io/finalizer"})
					if err := cli.Update(ctx, fresh, opts...); err != nil {
						return err
					}
					return k8serr.NewConflict(schema.GroupResource{Group: dataModuleCRGVK.Group, Resource: "feastoperators"}, current.GetName(), errors.New("replaced"))
				}
				return cli.Update(ctx, current, opts...)
			},
		}).Build()
		uid, err := addModuleCleanupFinalizer(t.Context(), cli, obj)
		require.NoError(t, err)
		require.Equal(t, types.UID("replacement"), uid)
	})
	t.Run("lost update response", func(t *testing.T) {
		obj := newModuleCleanupTestObject("original")
		lost := errors.New("lost response")
		first := true
		cli := fake.NewClientBuilder().WithObjects(obj.DeepCopy()).WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, cli client.WithWatch, current client.Object, opts ...client.UpdateOption) error {
				if err := cli.Update(ctx, current, opts...); err != nil {
					return err
				}
				if first {
					first = false
					return lost
				}
				return nil
			},
		}).Build()
		uid, err := addModuleCleanupFinalizer(t.Context(), cli, obj)
		require.ErrorIs(t, err, lost)
		require.Equal(t, types.UID("original"), uid)
		require.NoError(t, releaseModuleCleanupFinalizer(t.Context(), cli, obj, uid))
		require.NoError(t, cli.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
		require.Equal(t, []string{"platform.opendatahub.io/finalizer"}, obj.GetFinalizers())
	})
}
