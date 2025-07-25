package webhookutils

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

// ValidateConnectionAnnotation validates the connection annotation  "opendatahub.io/connections"
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
//   - *corev1.Secret: The validated secret (only valid when injection should be performed)
//   - string: The connection type (only valid when injection should be performed)
func ValidateConnectionAnnotation(ctx context.Context,
	cli client.Reader,
	decoder admission.Decoder,
	req admission.Request,
	allowedTypes []string,
) (admission.Response, bool, *corev1.Secret, string) {
	log := logf.FromContext(ctx)

	// Decode the InferenceService object from the request
	obj := &unstructured.Unstructured{}
	if err := decoder.Decode(req, obj); err != nil {
		log.Error(err, "failed to decode InferenceService object")
		return admission.Errored(http.StatusInternalServerError, err), false, nil, ""
	}

	// Get annotations from the request	object
	objAnnotations := obj.GetAnnotations()
	if objAnnotations == nil {
		objAnnotations = make(map[string]string)
	}

	// Check if the annotation "opendatahub.io/connections" exists and has a non-empty value
	annotationValue, exists := objAnnotations[annotations.Connection]
	if !exists || annotationValue == "" {
		// If annotation doesn't exist or is empty, allow the operation skip injection
		return admission.Allowed(fmt.Sprintf("Annotation '%s' not present or empty value, skipping validation", annotations.Connection)), false, nil, ""
	}

	// If annotation exists and has a value, validate the secret value
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      annotationValue,
		Namespace: req.Namespace,
	}
	if err := cli.Get(ctx, secretKey, secret); err != nil {
		if k8serr.IsNotFound(err) {
			return admission.Denied(fmt.Sprintf("Secret '%s' referenced in annotation '%s' not found in namespace '%s'",
				annotationValue, annotations.Connection, req.Namespace)), false, nil, ""
		}
		log.Error(err, "failed to get secret", "secretName", annotationValue, "namespace", req.Namespace)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to validate secret: %w", err)), false, nil, ""
	}

	// Additional validation: check the secret's connections-type-ref annotation
	secretAnnotations := secret.GetAnnotations()
	if secretAnnotations == nil {
		secretAnnotations = make(map[string]string)
	}
	connectionType, hasTypeAnnotation := secretAnnotations[annotations.ConnectionTypeRef]
	if !hasTypeAnnotation || connectionType == "" {
		// If annotation doesn't exist or is empty, allow the operation but no injection
		return admission.Allowed(fmt.Sprintf("Secret '%s' does not have '%s' annotation", annotationValue, annotations.ConnectionTypeRef)), false, nil, ""
	}
	// Validate that the connection type is one of the allowed values
	// TODO: we can extend this if we have new types in the future
	isValidType := false
	for _, allowedType := range allowedTypes {
		if connectionType == allowedType {
			isValidType = true
			break
		}
	}

	if !isValidType {
		// Allow unknown connection types but log a warning and don't perform injection
		log.Info("Unknown connection type found, allowing operation but skipping injection", "connectionType", connectionType, "allowedTypes", allowedTypes)
		return admission.Allowed(fmt.Sprintf("Annotation '%s' validation on secret '%s' with unknown type '%s' in namespace '%s'",
			annotations.Connection, annotationValue, connectionType, req.Namespace)), false, nil, ""
	}

	// Allow the operation and indicate that injection should be performed
	return admission.Allowed(fmt.Sprintf("Annotation '%s' validation passed for secret in namespace '%s'", annotations.Connection, req.Namespace)), true, secret, connectionType
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
