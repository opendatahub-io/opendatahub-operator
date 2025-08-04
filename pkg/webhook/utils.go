package webhookutils

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-logr/logr"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// NewWebhookLogConstructor returns a log constructor function for admission webhooks that adds the webhook name to the logger context for each admission request.
//
// Parameters:
//   - name: The name of the webhook to include in the logger context.
//
// Returns:
//   - func(logr.Logger, *admission.Request) logr.Logger: A function that constructs a logger with the webhook name and admission request context.
func NewWebhookLogConstructor(name string) func(logr.Logger, *admission.Request) logr.Logger {
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

// ValidateSingletonCreation denies creation if another instance of the same kind already exists (singleton enforcement).
//
// Parameters:
//   - ctx: Context for the API call (logger is extracted from here).
//   - cli: The controller-runtime reader to use for listing objects.
//   - req: The admission request being processed.
//   - expectedKind: The expected Kind string for validation.
//
// Returns:
//   - admission.Response: Errored if kind does not match, Denied if duplicate exists, Allowed otherwise.
func ValidateSingletonCreation(ctx context.Context, cli client.Reader, req *admission.Request, expectedKind string) admission.Response {
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

// DecodeUnstructured decodes an admission request into an unstructured object.
func DecodeUnstructured(decoder admission.Decoder, req admission.Request) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := decoder.Decode(req, obj); err != nil {
		return nil, fmt.Errorf("failed to decode object: %w", err)
	}
	return obj, nil
}

// ValidateInferenceServiceConnectionAnnotation validates the connection annotation  "opendatahub.io/connections"
// If the annotation exists and has a non-empty value, it validates that the value references
// a valid secret in the same namespace. Additionally, it checks the secret's connection type
// annotation and rejects requests with invalid configurations. (see allowedTypes)
// If the annotation doesn't exist or is empty, it allows the operation.
//
// Parameters:
//   - ctx: Context for the API call (logger is extracted from here).
//   - cli: The controller-runtime reader to use for getting secrets.
//   - decoder: The admission decoder to decode the request object.
//   - req: The admission request being processed.
//   - allowedTypes: List of allowed connection types for validation.
//
// Returns:
//   - admission.Response: The validation result
//   - bool: true if injection should be performed (only for known valid connection types)
//   - string: The validated secret name (only valid when injection should be performed)
//   - string: The connection type (only valid when injection should be performed)
func ValidateInferenceServiceConnectionAnnotation(ctx context.Context,
	cli client.Reader,
	decodedObj *unstructured.Unstructured,
	req admission.Request,
	allowedTypes []string,
) (admission.Response, bool, string, string) {
	log := logf.FromContext(ctx)

	// Check if the annotation "opendatahub.io/connections" exists and has a non-empty value
	annotationValue := resources.GetAnnotation(decodedObj, annotations.Connection)
	if annotationValue == "" {
		return admission.Allowed(fmt.Sprintf("Annotation '%s' not present or empty value, skipping validation", annotations.Connection)), false, "", ""
	}

	// Get the secret's metadata only (PartialObjectMetadata) to check annotations
	secretMeta := resources.GvkToPartial(gvk.Secret)
	if err := cli.Get(ctx, types.NamespacedName{Name: annotationValue, Namespace: req.Namespace}, secretMeta); err != nil {
		if k8serr.IsNotFound(err) {
			return admission.Denied(fmt.Sprintf("Secret '%s' referenced in annotation '%s' not found in namespace '%s'",
				annotationValue, annotations.Connection, req.Namespace)), false, "", ""
		}
		log.Error(err, "failed to get secret metadata", "secretName", annotationValue, "namespace", req.Namespace)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to validate secret: %w", err)), false, "", ""
	}

	// Additional validation: check the secret's connections-type-ref annotation exists and has a non-empty value
	connectionType := resources.GetAnnotation(secretMeta, annotations.ConnectionTypeRef)
	if connectionType == "" {
		return admission.Allowed(fmt.Sprintf("Secret '%s' does not have '%s' annotation", annotationValue, annotations.ConnectionTypeRef)), false, "", ""
	}
	// Validate that the connection type is one of the allowed values
	isValidType := slices.Contains(allowedTypes, connectionType)

	if !isValidType {
		// Allow unknown connection types but log a warning and don't perform injection
		log.Info("Unknown connection type found, allowing operation but skipping injection", "connectionType", connectionType, "allowedTypes", allowedTypes)
		return admission.Allowed(fmt.Sprintf("Annotation '%s' validation on secret '%s' with unknown type '%s' in namespace '%s'",
			annotations.Connection, annotationValue, connectionType, req.Namespace)), false, "", ""
	}

	// Allow the operation and indicate that injection should be performed
	return admission.Allowed("Connection annotation validation passed"), true, secretMeta.Name, connectionType
}

// GetOrCreateNestedMap safely retrieves or creates a nested map within an unstructured object.
// This utility function handles the common pattern of accessing nested maps in Kubernetes
// resource specifications, creating them if they don't exist.
//
// Parameters:
//   - obj: The parent map containing the nested field
//   - field: The field name to access or create
//
// Returns:
//   - map[string]interface{}: The existing or newly created nested map
//   - error: Any error encountered during map access or creation
func GetOrCreateNestedMap(obj map[string]interface{}, field string) (map[string]interface{}, error) {
	nested, found, err := unstructured.NestedMap(obj, field)
	if err != nil {
		return nil, fmt.Errorf("failed to get nested map for field %s: %w", field, err)
	}
	if !found {
		nested = make(map[string]interface{})
	}
	return nested, nil
}

// GetOrCreateNestedSlice gets a nested slice from an unstructured object, creating it if it doesn't exist.
func GetOrCreateNestedSlice(obj map[string]interface{}, path ...string) ([]interface{}, error) {
	nested, found, err := unstructured.NestedSlice(obj, path...)
	if err != nil {
		return nil, fmt.Errorf("failed to get nested slice for path %v: %w", path, err)
	}
	if !found {
		nested = make([]interface{}, 0)
	}
	return nested, nil
}

// SetNestedValue sets a nested value in an unstructured object based on the field type.
// This function unifies SetNestedField, SetNestedMap, SetNestedSlice.
//
// Parameters:
//   - obj: The unstructured object to modify
//   - value: The value to set
//   - path: The path to the field to set
//
// Returns:
//   - error: Any error encountered during the operation
func SetNestedValue(obj map[string]interface{}, value interface{}, path []string) error {
	switch v := value.(type) {
	case string:
		return unstructured.SetNestedField(obj, v, path...)
	case map[string]interface{}:
		return unstructured.SetNestedMap(obj, v, path...)
	case []interface{}:
		return unstructured.SetNestedSlice(obj, v, path...)
	default:
		return fmt.Errorf("unsupported value type %T for SetNestedValue", value)
	}
}
