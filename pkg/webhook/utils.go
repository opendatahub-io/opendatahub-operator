package webhookutils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// BaseServingConnectionWebhook provides common type for both isvc and llmisvc webhooks.
type BaseServingConnectionWebhook struct {
	APIReader client.Reader
	Client    client.Client
	Decoder   admission.Decoder
	Name      string
}

type ConnectionAction string

const (
	// ConnectionActionInject represents injecting connection.
	ConnectionActionInject ConnectionAction = "inject"
	// ConnectionActionRemove represents removing previously injected connection.
	ConnectionActionRemove ConnectionAction = "remove"
	// ConnectionActionReplace represents replacing one connection type with another.
	ConnectionActionReplace ConnectionAction = "replace"
	// ConnectionActionNone represents no action needed.
	ConnectionActionNone ConnectionAction = "none"
)

func (ca ConnectionAction) String() string {
	return string(ca)
}

// ConnectionInfo holds connection-related information for webhooks.
type ConnectionInfo struct {
	SecretName string // name of secret from annotation connections
	Type       string // value of the connection-type-ref annotation from secret
	Path       string // value of the connection-path annotation
}

// IsSecretEmpty returns true if no secret.
func (ci ConnectionInfo) IsSecretEmpty() bool {
	return ci.SecretName == ""
}

type ConnectionType string

const (
	// ConnectionTypeProtocolURI represents uri connections.
	ConnectionTypeProtocolURI ConnectionType = "uri"
	// ConnectionTypeProtocolS3 represents s3 connections.
	ConnectionTypeProtocolS3 ConnectionType = "s3"
	// ConnectionTypeProtocolOCI represents oci connections.
	ConnectionTypeProtocolOCI ConnectionType = "oci"
)

// ConnectionTypeRef constants are deprecated in favor of ConnectionTypeProtocol constants.
// Use ConnectionTypeProtocolURI, ConnectionTypeProtocolS3, and ConnectionTypeProtocolOCI instead.
const (
	// ConnectionTypeRefURI represents uri connections.
	ConnectionTypeRefURI ConnectionType = "uri-v1"
	// ConnectionTypeRefS3 represents s3 connections.
	ConnectionTypeRefS3 ConnectionType = "s3"
	// ConnectionTypeRefOCI represents oci connections.
	ConnectionTypeRefOCI ConnectionType = "oci-v1"
)

func (ct ConnectionType) String() string {
	return string(ct)
}

// CreateServiceAccount creates a ServiceAccount and links the secret.
func CreateServiceAccount(ctx context.Context, cli client.Client, secretName, namespace string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName + "-sa",
			Namespace: namespace,
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: secretName,
			},
		},
	}

	// only create if not exist, we do not reconcile object, as secret and SA can be both user managed
	err := cli.Create(ctx, sa)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create ServiceAccount: %w", err)
	}
	return nil
}

// HandleServiceAccountCreation handles ServiceAccount creation for S3 connections with proper logging.
// Returns an error if ServiceAccount creation fails, nil otherwise.
func HandleServiceAccountCreation(ctx context.Context, cli client.Client, secretName, connectionType, namespace string, isDryRun bool) error {
	log := logf.FromContext(ctx)

	switch {
	case (connectionType == ConnectionTypeProtocolS3.String() || connectionType == ConnectionTypeRefS3.String()) && !isDryRun:
		if err := CreateServiceAccount(ctx, cli, secretName, namespace); err != nil {
			log.Error(err, "Failed to create ServiceAccount for new S3 connection")
			return err
		}
	case (connectionType == ConnectionTypeProtocolS3.String() || connectionType == ConnectionTypeRefS3.String()) && isDryRun:
		log.V(1).Info("Skipping ServiceAccount creation in dry-run mode", "secretName", secretName)
	default:
		log.V(1).Info("Skipping ServiceAccount creation for non-S3 connection type", "connectionType", connectionType, "secretName", secretName)
	}

	return nil
}

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

// DecodeUnstructured decodes an admission request into an unstructured object.
func DecodeUnstructured(decoder admission.Decoder, req admission.Request) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := decoder.Decode(req, obj); err != nil {
		return nil, fmt.Errorf("failed to decode object: %w", err)
	}
	return obj, nil
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

// ValidateServingConnectionAnnotation validates the connection annotation  "opendatahub.io/connections"
// If the annotation exists and has a non-empty value, it validates that the value references
// a valid secret in the same namespace. Additionally, it checks the secret's connection-type-protocol and connection-type-ref
// annotation and rejects requests with invalid configurations. (see allowedTypes)
// If the annotation doesn't exist or is empty, it allows the operation.
//
// Parameters:
//   - ctx: Context for the API call (logger is extracted from here).
//   - cli: The controller-runtime reader to use for getting secrets.
//   - decodedObj: The decoded unstructured object.
//   - req: The admission request being processed.
//   - allowedTypes: Map of allowed connection types for validation.
//
// Returns:
//   - admission.Response: The validation result
//   - ConnectionInfo: The validated connection info (empty if no annotation or validation failed)
func ValidateServingConnectionAnnotation(ctx context.Context,
	cli client.Reader,
	decodedObj *unstructured.Unstructured,
	req admission.Request,
	allowedTypes map[string][]string,
) (admission.Response, ConnectionInfo) {
	log := logf.FromContext(ctx)

	// Check if the annotation "opendatahub.io/connections" exists and if it has an empty value, allow operation but return empty secret info
	annotationValue := resources.GetAnnotation(decodedObj, annotations.Connection)
	if annotationValue == "" {
		return admission.Allowed(fmt.Sprintf("Annotation '%s' not present or empty value", annotations.Connection)), ConnectionInfo{}
	}

	// Get the secret's metadata only (PartialObjectMetadata) to check annotations
	secretMeta := resources.GvkToPartial(gvk.Secret)
	if err := cli.Get(ctx, types.NamespacedName{Name: annotationValue, Namespace: req.Namespace}, secretMeta); err != nil {
		if k8serr.IsNotFound(err) {
			return admission.Denied(fmt.Sprintf("Secret '%s' referenced in annotation '%s' not found in namespace '%s'",
				annotationValue, annotations.Connection, req.Namespace)), ConnectionInfo{}
		}
		log.Error(err, "failed to get secret metadata", "secretName", annotationValue, "namespace", req.Namespace)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to validate secret: %w", err)), ConnectionInfo{}
	}

	// Validate the connection type
	connectionType, isValidType := ValidateInferenceServiceConnectionType(secretMeta, allowedTypes)

	// If neither connection type is present, allow the operation but skip injection
	if connectionType == "" {
		log.Info(fmt.Sprintf("Secret does not have '%s' or '%s' annotation, allowing operation but skipping injection",
			annotations.ConnectionTypeProtocol, annotations.ConnectionTypeRef), "connectionType", connectionType, "allowedTypes", allowedTypes)
		return admission.Allowed(fmt.Sprintf("Secret '%s' does not have '%s' or '%s' annotation",
			annotationValue, annotations.ConnectionTypeProtocol, annotations.ConnectionTypeRef)), ConnectionInfo{}
	}

	// Allow unknown connection types but log a warning and skip injection
	if !isValidType {
		log.Info("Unknown connection type found, allowing operation but skipping injection", "connectionType", connectionType, "allowedTypes", allowedTypes)
		return admission.Allowed(fmt.Sprintf("Annotation '%s' validation on secret '%s' with unknown type '%s' in namespace '%s'",
			annotations.Connection, annotationValue, connectionType, req.Namespace)), ConnectionInfo{}
	}

	connectionPath := GetS3Path(decodedObj)

	// Allow the operation and return connection info
	return admission.Allowed("Connection annotation validation passed"), ConnectionInfo{
		SecretName: secretMeta.Name,
		Type:       connectionType,
		Path:       connectionPath,
	}
}

// ValidateInferenceServiceConnectionType fetches the connection type from the secret metadata and validates it against the allowed types.
// It first checks the secret's connection type protocol annotation "opendatahub.io/connection-type-protocol".
// If the connection type protocol annotation doesn't exist, it falls back to the deprecated connection type ref
// annotation "opendatahub.io/connection-type-ref".
// If neither annotation exists, it returns an empty connection type.
//
// Parameters:
//   - secretMeta: The secret metadata to validate.
//   - allowedTypes: Map of allowed connection types for validation.
//
// Returns:
//   - string: The connection type (empty if no annotation)
//   - bool: True if the connection type is in the allowed types, false otherwise
func ValidateInferenceServiceConnectionType(secretMeta *metav1.PartialObjectMetadata, allowedTypes map[string][]string) (string, bool) {
	// First check the connection type protocol annotation
	connectionType := resources.GetAnnotation(secretMeta, annotations.ConnectionTypeProtocol)
	if connectionType != "" {
		// If it exists, check that the connection type is one of the allowed values
		isValidType := slices.Contains(allowedTypes[annotations.ConnectionTypeProtocol], connectionType)
		return connectionType, isValidType
	}

	// If the connection type protocol annotation doesn't exist, check the deprecated connection type ref annotation
	connectionType = resources.GetAnnotation(secretMeta, annotations.ConnectionTypeRef)
	if connectionType != "" {
		// If it exists, check that the connection type is one of the allowed values
		isValidType := slices.Contains(allowedTypes[annotations.ConnectionTypeRef], connectionType)
		return connectionType, isValidType
	}

	// If neither annotation exists, return empty connection type
	return "", false
}

// DetermineConnectionChangeAction determines what action to take for UPDATE operations
// by comparing old vs new connection info.
func DetermineConnectionChangeAction(oldConn, newConn ConnectionInfo) ConnectionAction {
	// Old connection, no new connection => remove
	if !oldConn.IsSecretEmpty() && newConn.IsSecretEmpty() {
		return ConnectionActionRemove
	}

	// No old connection, no new connection => none
	if oldConn.IsSecretEmpty() && newConn.IsSecretEmpty() {
		return ConnectionActionNone
	}

	// No old connection, new connection => inject
	if oldConn.IsSecretEmpty() && !newConn.IsSecretEmpty() {
		return ConnectionActionInject
	}

	// if type changed or secret changed => replace
	if oldConn.Type != newConn.Type || oldConn.SecretName != newConn.SecretName {
		return ConnectionActionReplace
	}

	// if connection-path changed for S3 connections => replace
	if (newConn.Type == ConnectionTypeRefS3.String() || newConn.Type == ConnectionTypeProtocolS3.String()) && oldConn.Path != newConn.Path {
		return ConnectionActionReplace
	}

	// no change needed.
	return ConnectionActionNone
}

// GetS3Path extracts the connection path from the opendatahub.io/connection-path annotation.
func GetS3Path(obj *unstructured.Unstructured) string {
	return resources.GetAnnotation(obj, annotations.ConnectionPath)
}

// CreateSA creates a ServiceAccount and links the secret.
func CreateSA(ctx context.Context, cli client.Client, secretName, namespace string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName + "-sa",
			Namespace: namespace,
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: secretName,
			},
		},
	}

	// only create if not exist, we do not reconcile object, as secret and SA can be both user managed
	err := cli.Create(ctx, sa)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create ServiceAccount: %w", err)
	}
	return nil
}

// ServiceAccountCreation handles ServiceAccount creation based on connection type.
func ServiceAccountCreation(ctx context.Context, cli client.Client, secretName, connectionType, namespace string, isDryRun bool) error {
	log := logf.FromContext(ctx)

	isS3Type := connectionType == ConnectionTypeRefS3.String() || connectionType == ConnectionTypeProtocolS3.String()

	switch {
	// TODO: add OCI type later.
	case isS3Type && !isDryRun:
		if err := CreateSA(ctx, cli, secretName, namespace); err != nil {
			log.Error(err, "Failed to create ServiceAccount for new S3 connection")
			return err
		}
	case isS3Type && isDryRun:
		log.V(1).Info("Skipping ServiceAccount creation in dry-run mode", "secretName", secretName)
	default:
		log.V(1).Info("Skipping ServiceAccount creation for non-S3 connection type", "connectionType", connectionType, "secretName", secretName)
	}

	return nil
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

// WebhookPrecheck do basic checks to ensure the webhook is properly initialized.
func (w *BaseServingConnectionWebhook) WebhookPrecheck(ctx context.Context, req admission.Request) *admission.Response {
	log := logf.FromContext(ctx)

	// Check if request object is valid
	if req.Object.Raw == nil {
		log.Error(nil, "Request object is nil")
		resp := admission.Errored(http.StatusBadRequest, errors.New("request object is nil"))
		return &resp
	}

	if w.Client == nil {
		log.Error(nil, "Client is nil - webhook not properly initialized")
		resp := admission.Errored(http.StatusInternalServerError, errors.New("webhook client not initialized"))
		return &resp
	}

	if w.APIReader == nil {
		log.Error(nil, "APIReader is nil - webhook not properly initialized")
		resp := admission.Errored(http.StatusInternalServerError, errors.New("webhook APIReader not initialized"))
		return &resp
	}

	if w.Decoder == nil {
		log.Error(nil, "Decoder is nil - webhook not properly initialized")
		resp := admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
		return &resp
	}

	return nil
}

// GetOldConnectionInfo extracts connection information from the old object in an admission request.
// This is used during UPDATE operations to determine if connection info has changed.
func (w *BaseServingConnectionWebhook) GetOldConnectionInfo(ctx context.Context, req admission.Request) (ConnectionInfo, error) {
	log := logf.FromContext(ctx)

	// Decode the old object
	oldObj := &unstructured.Unstructured{}
	if err := w.Decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
		log.Error(err, "failed to decode old object")
		return ConnectionInfo{}, fmt.Errorf("failed to decode old object: %w", err)
	}

	// Get old annotation value
	oldAnnotationValue := resources.GetAnnotation(oldObj, annotations.Connection)
	if oldAnnotationValue == "" {
		return ConnectionInfo{}, nil // No old connection
	}

	// Get old connection type from the secret
	secretMeta := resources.GvkToPartial(gvk.Secret)
	if err := w.APIReader.Get(ctx, types.NamespacedName{Name: oldAnnotationValue, Namespace: req.Namespace}, secretMeta); err != nil {
		if k8serr.IsNotFound(err) { // secret itself might be deleted already.
			log.V(1).Info("Old secret not found, but still need to cleanup references", "secretName", oldAnnotationValue)
			oldConnectionPath := resources.GetAnnotation(oldObj, annotations.ConnectionPath)
			return ConnectionInfo{
				SecretName: oldAnnotationValue,
				Type:       "", // we won't know which connection-type-ref was set on a already deleted secret
				Path:       oldConnectionPath,
			}, nil
		}
		return ConnectionInfo{}, fmt.Errorf("failed to get old secret metadata: %w", err)
	}

	oldConnectionType := resources.GetAnnotation(secretMeta, annotations.ConnectionTypeRef)
	oldConnectionPath := resources.GetAnnotation(oldObj, annotations.ConnectionPath)

	return ConnectionInfo{
		SecretName: oldAnnotationValue,
		Type:       oldConnectionType,
		Path:       oldConnectionPath,
	}, nil
}

// HandleSA injects or removes serviceaccount from the specified path.
// If saName is empty, it removes the field entirely.
func (w *BaseServingConnectionWebhook) HandleSA(obj *unstructured.Unstructured, path []string, saName string) error {
	// Get the current value at the path
	currentSAName, found, err := unstructured.NestedString(obj.Object, path...)
	if err != nil {
		return fmt.Errorf("failed to get serviceAccountName from path %v: %w", path, err)
	}

	// Remove the field entirely if saName is empty
	if saName == "" {
		// Only remove if the field exists, if it does not exist, or it has a different value(manual set by user), do nothing.
		if found {
			unstructured.RemoveNestedField(obj.Object, path...)
		}
		return nil
	}

	// Only set the value if it's different from the current value
	if !found || currentSAName != saName {
		return SetNestedValue(obj.Object, saName, path)
	}

	// Value is already set correctly, no action needed
	return nil
}

// InjectOCIImagePullSecrets injects imagePullSecrets for OCI connections.
func (w *BaseServingConnectionWebhook) InjectOCIImagePullSecrets(obj *unstructured.Unstructured, path []string, secretName string) error {
	imagePullSecrets, err := GetOrCreateNestedSlice(obj.Object, path...)
	if err != nil {
		return fmt.Errorf("failed to get path: %w", err)
	}

	// Check if the secret is already in the list, fast exist
	for _, secret := range imagePullSecrets {
		if secretMap, ok := secret.(map[string]interface{}); ok {
			if name, exists := secretMap["name"]; exists && name == secretName {
				return nil
			}
		}
	}

	// Add new secret to the slice(upon UPDATE)
	newImagePullSecret := map[string]interface{}{
		"name": secretName,
	}
	imagePullSecrets = append(imagePullSecrets, newImagePullSecret)

	return SetNestedValue(obj.Object, imagePullSecrets, path)
}

// CleanupOCIImagePullSecrets removes the specified secret from imagePullSecrets array.
// If secretName is empty, does nothing.
// If the array becomes empty after removal, the entire field is removed.
func (w *BaseServingConnectionWebhook) CleanupOCIImagePullSecrets(obj *unstructured.Unstructured, path []string, secretName string) error {
	if secretName == "" {
		return nil
	}

	imagePullSecrets, found, err := unstructured.NestedSlice(obj.Object, path...)
	if err != nil {
		return fmt.Errorf("failed to get imagePullSecrets: %w", err)
	}
	if !found {
		return nil
	}

	var remained []interface{}
	for _, secret := range imagePullSecrets {
		if secretMap, ok := secret.(map[string]interface{}); ok {
			if name, exists := secretMap["name"]; exists && name != secretName {
				remained = append(remained, secret)
			}
		}
	}

	// Handle the result
	if len(remained) == 0 {
		// No secrets left, remove the entire field
		unstructured.RemoveNestedField(obj.Object, path...)
	} else {
		// Update with filtered list
		err = unstructured.SetNestedSlice(obj.Object, remained, path...)
		if err != nil {
			return fmt.Errorf("failed to set filtered imagePullSecrets: %w", err)
		}
	}

	return nil
}

// CreatePatchResponse creates a patch response from the modified object.
func (w *BaseServingConnectionWebhook) CreatePatchResponse(req admission.Request, obj *unstructured.Unstructured) admission.Response {
	marshaledObj, err := json.Marshal(obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
}

// GetURIValue extracts URI value from the secret for URI-type connections.
func (w *BaseServingConnectionWebhook) GetURIValue(ctx context.Context, obj *unstructured.Unstructured, secretName, namespace string) (string, error) {
	// Fetch the secret to get the URI data
	secret := &corev1.Secret{}
	if err := w.APIReader.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	// get URI from either "https-host" or "URI" key
	var uriHost []byte
	var exists bool

	if uriHost, exists = secret.Data["https-host"]; !exists {
		if uriHost, exists = secret.Data["URI"]; !exists {
			return "", errors.New("secret does not contain either 'https-host' or 'URI' data key")
		}
	}
	return string(uriHost), nil
}

// BuildS3URI constructs S3 URI value from the secret and connection path annotation.
// Returns URI in the format: s3://<AWS_BUCKET>/$annotation.connection-path.
func (w *BaseServingConnectionWebhook) BuildS3URI(ctx context.Context, connInfo ConnectionInfo, namespace string) (string, error) {
	// Fetch the secret to get the S3 bucket data
	secret := &corev1.Secret{}
	if err := w.APIReader.Get(ctx, types.NamespacedName{Name: connInfo.SecretName, Namespace: namespace}, secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", connInfo.SecretName, err)
	}
	bucketName, exists := secret.Data["AWS_S3_BUCKET"]
	if !exists {
		return "", errors.New("secret does not contain 'AWS_S3_BUCKET' data key")
	}
	if len(bucketName) == 0 {
		return "", errors.New("secret 'AWS_S3_BUCKET' data key is empty, cannot use it for s3:// to get model")
	}

	if connInfo.Path == "" {
		return "", errors.New("connection info does not have path")
	}

	s3URI := fmt.Sprintf("s3://%s/%s", string(bucketName), connInfo.Path)
	return s3URI, nil
}
