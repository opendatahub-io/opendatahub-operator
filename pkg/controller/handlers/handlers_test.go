package handlers_test

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestNewEventHandlerForGVK(t *testing.T) {
	t.Run("should return empty requests when no resources exist", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		eh := handlers.NewEventHandlerForGVK(cli, gvk.Namespace)

		q := controllertest.Queue{
			TypedInterface: workqueue.NewTyped[reconcile.Request](),
		}

		eh.Create(
			ctx,
			event.CreateEvent{},
			&q)

		g.Expect(q.Len()).To(BeZero())
	})

	t.Run("should enqueue requests for existing resources", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		// Create test resources using GvkToUnstructured
		obj1 := resources.GvkToUnstructured(gvk.Namespace)
		obj1.SetName("test-ns-1")

		obj2 := resources.GvkToUnstructured(gvk.Namespace)
		obj2.SetName("test-ns-2")

		// Create fake client with objects
		cli, err := fakeclient.New(fakeclient.WithObjects(obj1, obj2))
		g.Expect(err).ShouldNot(HaveOccurred())

		eh := handlers.NewEventHandlerForGVK(cli, gvk.Namespace)

		q := controllertest.Queue{
			TypedInterface: workqueue.NewTyped[reconcile.Request](),
		}

		eh.Create(
			ctx,
			event.CreateEvent{},
			&q)

		// Verify both resources were enqueued
		g.Expect(q.Len()).To(Equal(2))

		// Get and verify each request individually
		g.Expect(q.Get()).To(Equal(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-ns-1"},
		}))

		g.Expect(q.Get()).To(Equal(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-ns-2"},
		}))
	})

	t.Run("should only enqueue requests for matching GVK", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		// Create resources with different GVKs
		nsObj := resources.GvkToUnstructured(gvk.Namespace)
		nsObj.SetName("test-ns")

		secretObj := resources.GvkToUnstructured(gvk.Secret)
		secretObj.SetName("test-secret")

		configMapObj := resources.GvkToUnstructured(gvk.ConfigMap)
		configMapObj.SetName("test-cm")

		// Create fake client with mixed objects
		cli, err := fakeclient.New(fakeclient.WithObjects(nsObj, secretObj, configMapObj))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create handler that only watches Namespace GVK
		eh := handlers.NewEventHandlerForGVK(cli, gvk.Namespace)

		q := controllertest.Queue{
			TypedInterface: workqueue.NewTyped[reconcile.Request](),
		}

		eh.Create(
			ctx,
			event.CreateEvent{},
			&q)

		// Verify only the namespace resource was enqueued
		g.Expect(q.Len()).To(Equal(1))

		// Get and verify the request
		g.Expect(q.Get()).To(Equal(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-ns"},
		}))
	})
}
