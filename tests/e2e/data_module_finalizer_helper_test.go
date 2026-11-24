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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestAddFinalizerToCurrentResourceCapturesFreshUID(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "replacement before first get"
		if conflict {
			name = "replacement during conflict retry"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const finalizer = "e2e.opendatahub.io/dch-cleanup-hold"
			const freshUID types.UID = "replacement-uid"
			application := &unstructured.Unstructured{}
			application.SetGroupVersionKind(schema.GroupVersionKind{
				Group: "dataconnecthub.opendatahub.io", Version: "v1alpha1", Kind: "DataConnectService",
			})
			application.SetName("default-dataconnectservice")
			application.SetNamespace("applications")
			application.SetUID("initial-uid")
			stored := application.DeepCopy()
			if !conflict {
				stored.SetUID(freshUID)
			}
			updates := 0
			cli := fake.NewClientBuilder().WithObjects(stored).WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					updates++
					if conflict && updates == 1 {
						require.NoError(t, cli.Delete(ctx, stored))
						replacement := stored.DeepCopy()
						replacement.SetUID(freshUID)
						replacement.SetResourceVersion("")
						require.NoError(t, cli.Create(ctx, replacement))
						return k8serr.NewConflict(schema.GroupResource{
							Group: "dataconnecthub.opendatahub.io", Resource: "dataconnectservices",
						}, obj.GetName(), errors.New("application replaced"))
					}
					return cli.Update(ctx, obj, opts...)
				},
			}).Build()
			heldUID, err := addFinalizerToCurrentResource(ctx, cli, application, finalizer)
			require.NoError(t, err)
			require.Equal(t, freshUID, heldUID)
			current := application.DeepCopy()
			require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(application), current))
			require.Equal(t, heldUID, current.GetUID())
			require.Contains(t, current.GetFinalizers(), finalizer)
		})
	}
}
