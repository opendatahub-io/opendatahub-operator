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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

// WithEventuallyTimeout sets the default timeout for Eventually assertions.
//
// Parameters:
//   - value (time.Duration): The timeout duration for Eventually assertions.
//
// Returns:
//   - WithTOpts: A function that applies the timeout configuration to a WithT instance.
func WithEventuallyTimeout(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultEventuallyTimeout(value)
	}
}

// WithEventuallyPollingInterval sets the default polling interval for Eventually assertions.
//
// Parameters:
//   - value (time.Duration): The polling interval for Eventually assertions.
//
// Returns:
//   - WithTOpts: A function that applies the polling interval configuration to a WithT instance.
func WithEventuallyPollingInterval(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultEventuallyPollingInterval(value)
	}
}

// WithConsistentlyDuration sets the default duration for Consistently assertions.
//
// Parameters:
//   - value (time.Duration): The duration for Consistently assertions.
//
// Returns:
//   - WithTOpts: A function that applies the duration configuration to a WithT instance.
func WithConsistentlyDuration(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultConsistentlyDuration(value)
	}
}

// WithConsistentlyPollingInterval sets the default polling interval for Consistently assertions.
//
// Parameters:
//   - value (time.Duration): The polling interval for Consistently assertions.
//
// Returns:
//   - WithTOpts: A function that applies the polling interval configuration to a WithT instance.
func WithConsistentlyPollingInterval(value time.Duration) WithTOpts {
	return func(g *WithT) {
		g.SetDefaultConsistentlyPollingInterval(value)
	}
}

// Context returns the current context associated with the test, used for resource operations.
//
// Returns:
//   - context.Context: The current context for the test, which can be used for Kubernetes operations.
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

// List performs a `kubectl get` operation to list resources of the specified GroupVersionKind.
// It returns the list of resources wrapped in an EventuallyValue to be used with Gomega assertions.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resources to list.
//   - option (...client.ListOption): Optional options to modify the list operation.
//
// Returns:
//   - *EventuallyValue[[]unstructured.Unstructured]: The eventually available list of resources wrapped in an EventuallyValue,
//     which can be used with Gomega assertions to test the list result.
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

// Get performs a `kubectl get` operation for the specified resource and returns the resource wrapped in an EventuallyValue.
// The result can be used in Gomega assertions to test the resource's existence and state.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to get.
//   - nn (types.NamespacedName): The namespace and name of the resource to get.
//   - option (...client.GetOption): Optional options for the get operation.
//
// Returns:
//   - *EventuallyValue[*unstructured.Unstructured]: The eventually available resource wrapped in an EventuallyValue,
//     which can be used with Gomega assertions to test the resource's state.
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

// Create performs a `kubectl create` operation to create the specified Kubernetes resource.
// It returns an EventuallyValue that wraps the created resource, which can be used with Gomega assertions.
//
// Parameters:
//   - obj (*unstructured.Unstructured): The resource to create. It must have the appropriate GroupVersionKind,
//     name, and namespace set in its metadata.
//   - nn (types.NamespacedName): The namespace and name of the resource. This should match the metadata in `obj`.
//   - option (...client.CreateOption): Optional client options for the create operation.
//
// Returns:
//   - *EventuallyValue[*unstructured.Unstructured]: The eventually available created resource, wrapped in an EventuallyValue,
//     which can be used with Gomega assertions to test the created resource.
func (t *WithT) Create(
	obj *unstructured.Unstructured,
	nn types.NamespacedName,
	option ...client.CreateOption,
) *EventuallyValue[*unstructured.Unstructured] {
	return &EventuallyValue[*unstructured.Unstructured]{
		ctx: t.Context(),
		g:   t.WithT,
		f: func(ctx context.Context) (*unstructured.Unstructured, error) {
			err := t.Client().Create(ctx, obj, option...)
			if err != nil {
				return nil, StopErr(err, "failed to create resource: %s, nn: %s", obj.GetObjectKind().GroupVersionKind(), nn.String())
			}

			return obj, nil
		},
	}
}

// CreateOrUpdate ensures the specified resource exists by either creating it if it does not exist,
// or updating it if it already exists. This method applies the provided mutation function to modify
// the resource before creating or updating it. The function wraps `controllerutil.CreateOrUpdate`,
// ensuring that the resource is created or updated atomically.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource.
//   - nn (types.NamespacedName): The namespace and name of the resource to be operated on.
//   - fn (func): An optional function that applies a mutation to the resource before creation or update.
//     If omitted, the resource is created or updated without modification.
//
// Returns:
//   - *EventuallyValue[*unstructured.Unstructured]: The eventually available resource after being created or updated,
//     wrapped for use with Gomega assertions.
func (t *WithT) CreateOrUpdate(
	obj *unstructured.Unstructured,
	fn ...func(obj *unstructured.Unstructured) error,
) *EventuallyValue[*unstructured.Unstructured] {
	return &EventuallyValue[*unstructured.Unstructured]{
		ctx: t.Context(),
		g:   t.WithT,
		f: func(ctx context.Context) (*unstructured.Unstructured, error) {
			// Use the provided fn or a default no-op if fn is not provided
			mutationFn := func() error {
				if len(fn) > 0 && fn[0] != nil {
					return fn[0](obj) // Use the provided mutation function
				}
				return nil // Default no-op function if fn is omitted
			}

			_, err := controllerutil.CreateOrUpdate(ctx, t.Client(), obj, mutationFn)

			switch {
			case errors.IsForbidden(err):
				return nil, StopErr(
					err,
					"failed to create or update resource: %s, nn: %s",
					obj.GetObjectKind().GroupVersionKind(),
					obj.GetNamespace(),
				)
			case err != nil:
				return nil, err
			default:
				return obj, nil
			}
		},
	}
}

// CreateOrPatch ensures the specified resource exists by either creating it if it does not exist,
// or applying a patch if the resource already exists. This function uses the
// `controllerutil.CreateOrPatch` method to handle the creation and patching operations atomically.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to be operated on.
//   - nn (types.NamespacedName): The namespace and name of the resource to be created or patched.
//   - fn (func): A function that modifies the resource before creating or patching it.
//
// Returns:
//   - *EventuallyValue[*unstructured.Unstructured]: The eventually available resource after being created or patched.
func (t *WithT) CreateOrPatch(
	obj *unstructured.Unstructured,
	fn ...func(obj *unstructured.Unstructured) error,
) *EventuallyValue[*unstructured.Unstructured] {
	return &EventuallyValue[*unstructured.Unstructured]{
		ctx: t.Context(),
		g:   t.WithT,
		f: func(ctx context.Context) (*unstructured.Unstructured, error) {
			// Use the provided fn or a default no-op if fn is not provided
			mutationFn := func() error {
				// Remove status fields that should not be modified directly
				unstructured.RemoveNestedField(obj.Object, "status")

				if len(fn) > 0 && fn[0] != nil {
					return fn[0](obj) // Use the provided mutation function
				}

				return nil // Default no-op function if fn is omitted
			}

			_, err := controllerutil.CreateOrPatch(ctx, t.Client(), obj, mutationFn)

			// Check for errors
			switch {
			case err != nil:
				return nil, StopErr(
					err,
					"failed to create or patch resource: %s, nn: %s",
					obj.GetObjectKind().GroupVersionKind(),
					obj.GetNamespace(),
				)
			default:
				return obj, nil // Successfully created or patched
			}
		},
	}
}

// Update performs a `kubectl update` operation on the specified resource, applying a function to mutate the resource
// before updating. The result is wrapped in an EventuallyValue, which can be used in Gomega assertions.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to update.
//   - nn (types.NamespacedName): The namespace and name of the resource to update.
//   - fn (func): A function that modifies the resource before updating.
//   - option (...client.UpdateOption): Optional options for the update operation.
//
// Returns:
//   - *EventuallyValue[*unstructured.Unstructured]: The eventually available resource wrapped in an EventuallyValue,
//     which can be used with Gomega assertions to test the updated resource.
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
			u := resources.GvkToUnstructured(gvk)

			err := t.Client().Get(ctx, nn, u)
			switch {
			case errors.IsNotFound(err):
				return nil, nil
			case err != nil:
				return nil, StopErr(err, "failed to get resource: %s, nn: %s", gvk, nn.String())
			}

			in, err := resources.ToUnstructured(u)
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

// Delete performs a `kubectl delete` operation on the specified resource.
// It returns an EventuallyErr, which can be used in Gomega assertions to check for the deletion's success or failure.
//
// Parameters:
//   - gvk (schema.GroupVersionKind): The GroupVersionKind of the resource to delete.
//   - nn (types.NamespacedName): The namespace and name of the resource to delete.
//   - option (...client.DeleteOption): Optional options for the delete operation.
//
// Returns:
//   - *EventuallyErr: The eventually available result of the delete operation, wrapped in an EventuallyErr,
//     which can be used with Gomega assertions to test the deletion result.
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
