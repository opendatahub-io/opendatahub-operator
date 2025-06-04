package shared

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// NewLogConstructor returns a log constructor function for admission webhooks that adds the webhook name to the logger context for each admission request.
//
// Parameters:
//   - name: The name of the webhook to include in the logger context.
//
// Returns:
//   - func(logr.Logger, *admission.Request) logr.Logger: A function that constructs a logger with the webhook name and admission request context.
func NewLogConstructor(name string) func(logr.Logger, *admission.Request) logr.Logger {
	return func(_ logr.Logger, req *admission.Request) logr.Logger {
		base := ctrl.Log
		l := admission.DefaultLogConstructor(base, req)

		if req == nil {
			return l.WithValues("webhook", name)
		}
		return l.WithValues(
			"webhook", name,
			"namespace", req.Namespace,
			"name", req.Name,
			"operation", req.Operation,
			"kind", req.Kind.Kind,
		)
	}
}

// CountObjects returns the number of objects of the given GroupVersionKind in the cluster.
//
// Parameters:
//   - ctx: Context for the API call.
//   - cli: The controller-runtime reader to use for listing objects.
//   - gvk: The GroupVersionKind of the objects to count.
//   - opts: Optional client.ListOption arguments for filtering, pagination, etc.
//
// Returns:
//   - int: The number of objects found.
//   - error: Any error encountered during the list operation.
func CountObjects(ctx context.Context, cli client.Reader, gvk schema.GroupVersionKind, opts ...client.ListOption) (int, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := cli.List(ctx, list, opts...); err != nil {
		return 0, err
	}

	return len(list.Items), nil
}

// DenyCountGtZero denies the admission request if there is at least one object of the given GroupVersionKind in the cluster.
//
// Parameters:
//   - ctx: Context for the API call.
//   - cli: The controller-runtime reader to use for listing objects.
//   - gvk: The GroupVersionKind to check for existing objects.
//   - msg: The denial message to return if objects are found.
//
// Returns:
//   - admission.Response: Denied if objects exist, Allowed otherwise, or Errored on failure.
func DenyCountGtZero(ctx context.Context, cli client.Reader, gvk schema.GroupVersionKind, msg string) admission.Response {
	count, err := CountObjects(ctx, cli, gvk)
	if err != nil {
		logf.FromContext(ctx).Error(err, "error listing objects")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if count > 0 {
		return admission.Denied(msg)
	}

	return admission.Allowed("")
}

// ValidateDupCreation denies creation if another instance of the same kind already exists (singleton enforcement).
//
// Parameters:
//   - ctx: Context for the API call (logger is extracted from here).
//   - cli: The controller-runtime reader to use for listing objects.
//   - req: The admission request being processed.
//   - expectedKind: The expected Kind string for validation.
//
// Returns:
//   - admission.Response: Errored if kind does not match, Denied if duplicate exists, Allowed otherwise.
func ValidateDupCreation(ctx context.Context, cli client.Reader, req *admission.Request, expectedKind string) admission.Response {
	if req.Kind.Kind != expectedKind {
		err := fmt.Errorf("unexpected kind: %s", req.Kind.Kind)
		logf.FromContext(ctx).Error(err, "got wrong kind")
		return admission.Errored(http.StatusBadRequest, err)
	}

	resourceGVK := schema.GroupVersionKind{
		Group:   req.Kind.Group,
		Version: req.Kind.Version,
		Kind:    req.Kind.Kind,
	}

	return DenyCountGtZero(ctx, cli, resourceGVK,
		fmt.Sprintf("Only one instance of %s object is allowed", req.Kind.Kind))
}
