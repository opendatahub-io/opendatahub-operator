//go:build !nowebhook

package inferenceservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

// create new type for connection types.
type ConnectionType string

const (
	// ConnectionTypeURI represents uri connections.
	ConnectionTypeURI ConnectionType = "uri-v1"
	// ConnectionTypeS3 represents s3 connections.
	ConnectionTypeS3 ConnectionType = "s3"
	// ConnectionTypeOCI represents oci connections.
	ConnectionTypeOCI ConnectionType = "oci-v1"
)

func (ct ConnectionType) String() string {
	return string(ct)
}

type InferenceServingPath struct {
	ModelPath              []string
	ImagePullSecretPath    []string
	StorageUriPath         []string
	ServiceAccountNamePath []string
}

var IsvcConfigs = InferenceServingPath{
	ModelPath:              []string{"spec", "predictor", "model"},               // used by S3, has map
	ImagePullSecretPath:    []string{"spec", "predictor", "imagePullSecrets"},    // used by OCI, has slice
	StorageUriPath:         []string{"spec", "predictor", "model", "storageUri"}, // used by URI, has string
	ServiceAccountNamePath: []string{"spec", "predictor", "serviceAccountName"},  // used by all, has string
}

//+kubebuilder:webhook:path=/platform-connection-isvc,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=inferenceservices,verbs=create;update,versions=v1beta1,name=connection-isvc.opendatahub.io,sideEffects=NoneOnDryRun,admissionReviewVersions=v1
//nolint:lll

type ConnectionWebhook struct {
	APIReader client.Reader
	Client    client.Client // used to create ServiceAccount
	Decoder   admission.Decoder
	Name      string
}

var _ admission.Handler = &ConnectionWebhook{}

func (w *ConnectionWebhook) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/platform-connection-isvc", &webhook.Admission{
		Handler:        w,
		LogConstructor: webhookutils.NewWebhookLogConstructor(w.Name),
	})
	return nil
}

func (w *ConnectionWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Check if request object is valid
	if req.Object.Raw == nil {
		log.Error(nil, "Request object is nil")
		return admission.Errored(http.StatusBadRequest, errors.New("request object is nil"))
	}

	if w.Client == nil {
		log.Error(nil, "Client is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook client not initialized"))
	}

	if w.APIReader == nil {
		log.Error(nil, "APIReader is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook APIReader not initialized"))
	}

	if w.Decoder == nil {
		log.Error(nil, "Decoder is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
	}
	// Decode the object once
	obj, err := webhookutils.DecodeUnstructured(w.Decoder, req)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Skip processing if object is marked for deletion
	if !obj.GetDeletionTimestamp().IsZero() {
		return admission.Allowed("Object marked for deletion, skipping connection logic")
	}

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:

		// allowed connection types for connection validation on isvc.
		allowedTypes := []string{
			ConnectionTypeURI.String(),
			ConnectionTypeS3.String(),
			ConnectionTypeOCI.String(),
		}

		// validate the connection annotation and determine the action to take
		validationResp, action, secretName, connectionType := webhookutils.ValidateInferenceServiceConnectionAnnotation(ctx, w.APIReader, obj, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		// Handle different actions based on the ConnectionAction value
		switch action {
		case webhookutils.ConnectionActionInject:
			// create ServiceAccount first (skip if it is dry-run)
			isDryRun := req.DryRun != nil && *req.DryRun
			if !isDryRun {
				if err := webhookutils.CreateServiceAccount(ctx, w.Client, secretName, req.Namespace); err != nil {
					log.Error(err, "Failed to create ServiceAccount")
					return admission.Errored(http.StatusInternalServerError, err)
				}
			} else {
				log.V(1).Info("Skipping ServiceAccount creation in dry-run mode", "secretName", secretName)
			}
			// Perform injection for valid connection types
			injectionPerformed, err := w.performConnectionInjection(ctx, req, secretName, connectionType, obj)
			if err != nil {
				log.Error(err, "Failed to perform connection injection")
				return admission.Errored(http.StatusInternalServerError, err)
			}

			// Write updated object back to k8s if injection was performed
			if injectionPerformed {
				marshaledObj, err := json.Marshal(obj)
				if err != nil {
					return admission.Errored(http.StatusInternalServerError, err)
				}
				return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
			}

			return admission.Allowed(fmt.Sprintf("No connection injection performed for %s in namespace %s", req.Kind.Kind, req.Namespace))

		case webhookutils.ConnectionActionRemove:
			// Perform cleanup when annotation is removed, we do not delete SA but only remove injection part
			cleanupPerformed, err := w.performConnectionCleanup(ctx, req, obj)
			if err != nil {
				log.Error(err, "Failed to perform connection cleanup")
				return admission.Errored(http.StatusInternalServerError, err)
			}

			if cleanupPerformed {
				marshaledObj, err := json.Marshal(obj)
				if err != nil {
					return admission.Errored(http.StatusInternalServerError, err)
				}
				return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
			}

			return admission.Allowed(fmt.Sprintf("Connection cleanup done for %s in namespace %s", req.Kind.Kind, req.Namespace))

		case webhookutils.ConnectionActionNone:
			// No action needed
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, no action needed", req.Namespace, req.Kind.Kind))

		default:
			log.V(1).Info("Unknown", "action", action)
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, unknown action: %s", req.Namespace, req.Kind.Kind, action))
		}

	default: // Delete operation
		return admission.Allowed(fmt.Sprintf("Operation %s on %s allowed in namespace %s", req.Operation, req.Kind.Kind, req.Namespace))
	}
}

func (w *ConnectionWebhook) performConnectionInjection(
	ctx context.Context,
	req admission.Request,
	secretName string,
	connectionType string,
	decodedObj *unstructured.Unstructured,
) (bool, error) {
	log := logf.FromContext(ctx)

	// inject ServiceAccount as common part
	if err := w.handleSA(decodedObj, secretName+"-sa"); err != nil {
		log.Error(err, "Failed to inject ServiceAccount")
		return false, fmt.Errorf("failed to inject ServiceAccount: %w", err)
	}
	log.V(1).Info("Successfully injected ServiceAccount", "ServiceAccountName", secretName+"-sa")

	// injection based on connection type
	switch ConnectionType(connectionType) {
	case ConnectionTypeOCI:
		if err := w.injectOCIImagePullSecrets(decodedObj, secretName); err != nil {
			return false, fmt.Errorf("failed to inject OCI imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully injected OCI imagePullSecrets", "secretName", secretName)
		return true, nil

	case ConnectionTypeURI:
		if err := w.injectURIStorageUri(ctx, decodedObj, secretName, req.Namespace); err != nil {
			return false, fmt.Errorf("failed to inject URI storageUri: %w", err)
		}
		log.V(1).Info("Successfully injected URI storageUri from secret", "secretName", secretName)
		return true, nil

	case ConnectionTypeS3:
		if err := w.injectS3StorageKey(decodedObj, secretName); err != nil {
			return false, fmt.Errorf("failed to inject S3 storage.key: %w", err)
		}
		log.V(1).Info("Successfully injected S3 storage key", "secretName", secretName)
		return true, nil

	default: // this should not enter since ValidateConnectionAnnotation ensures valid types, but keep it for safety
		log.V(1).Info("Unknown connection type, skipping injection", "connectionType", connectionType)
		return false, nil
	}
}

// performConnectionCleanup removes previously injected connection fields when the annotation is removed on UPDATE operation.
// all possible connection types are checked for cleanup.
func (w *ConnectionWebhook) performConnectionCleanup(
	ctx context.Context,
	req admission.Request,
	decodedObj *unstructured.Unstructured,
) (bool, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Performing connection cleanup for removed annotation", "name", req.Name, "namespace", req.Namespace)

	// remove ServiceAccountName injection
	if err := w.handleSA(decodedObj, ""); err != nil {
		log.Error(err, "Failed to cleanup ServiceAccountName")
		return false, fmt.Errorf("failed to cleanup ServiceAccountName: %w", err)
	}
	cleanupPerformed := true

	// Check for OCI imagePullSecrets
	if hasOCIImagePullSecrets(decodedObj) {
		if _, err := w.cleanupOCIImagePullSecrets(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup OCI imagePullSecrets", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup OCI imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully cleaned up OCI imagePullSecrets", "name", req.Name, "namespace", req.Namespace)
	}

	// Check for URI storageUri
	if hasURIStorageUri(decodedObj) {
		if _, err := w.cleanupURIStorageUri(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup URI storageUri", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup URI storageUri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up URI storageUri", "name", req.Name, "namespace", req.Namespace)
		cleanupPerformed = true
	}

	// Check for S3 storage key
	if hasS3StorageKey(decodedObj) {
		if _, err := w.cleanupS3StorageKey(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup S3 storage key", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup S3 storage key: %w", err)
		}
		log.V(1).Info("Successfully cleaned up S3 storage key", "name", req.Name, "namespace", req.Namespace)
		cleanupPerformed = true
	}

	return cleanupPerformed, nil
}

func hasOCIImagePullSecrets(obj *unstructured.Unstructured) bool {
	imagePullSecrets, found, err := unstructured.NestedSlice(obj.Object, IsvcConfigs.ImagePullSecretPath...)
	if err != nil || !found {
		return false
	}
	return len(imagePullSecrets) > 0 // if it is empty, we don't need to delete the whole shebang
}

func hasURIStorageUri(obj *unstructured.Unstructured) bool {
	storageUri, found, err := unstructured.NestedString(obj.Object, IsvcConfigs.StorageUriPath...)
	if err != nil || !found {
		return false
	}
	return storageUri != ""
}

func hasS3StorageKey(obj *unstructured.Unstructured) bool {
	_, found, err := unstructured.NestedMap(obj.Object, append(IsvcConfigs.ModelPath, "storage")...)
	if err != nil || !found {
		return false
	}

	return true
}

// cleanupOCIImagePullSecrets set empty slice to spec.predictor.imagePullSecrets.
func (w *ConnectionWebhook) cleanupOCIImagePullSecrets(obj *unstructured.Unstructured) (bool, error) {
	err := webhookutils.SetNestedValue(obj.Object, []interface{}{}, IsvcConfigs.ImagePullSecretPath)
	return true, err
}

// cleanupURIStorageUri delete the storageUri field from spec.predictor.model.
// cannot just set to empty string, it will fail in ValidateStorageURI().
func (w *ConnectionWebhook) cleanupURIStorageUri(obj *unstructured.Unstructured) (bool, error) {
	model, _, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return false, fmt.Errorf("failed to get spec.predictor.model: %w", err)
	}

	// Remove the storageUri field
	delete(model, "storageUri")

	err = webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath)
	return true, err
}

// cleanupS3StorageKey removes the storage field from spec.predictor.model.
func (w *ConnectionWebhook) cleanupS3StorageKey(obj *unstructured.Unstructured) (bool, error) {
	model, _, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return false, fmt.Errorf("failed to get spec.predictor.model: %w", err)
	}

	// Remove the storage field
	delete(model, "storage")

	err = webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath)
	return true, err
}

// handleSA injects serviceaccount into spec.predictor.serviceAccountName.
// if saName is "", it is removal.
func (w *ConnectionWebhook) handleSA(obj *unstructured.Unstructured, saName string) error {
	if saName == "" {
		// Remove the field entirely
		unstructured.RemoveNestedField(obj.Object, IsvcConfigs.ServiceAccountNamePath...)
		return nil
	}
	return webhookutils.SetNestedValue(obj.Object, saName, IsvcConfigs.ServiceAccountNamePath)
}

// injectOCIImagePullSecrets injects imagePullSecrets into spec.predictor.imagePullSecrets for OCI connections.
func (w *ConnectionWebhook) injectOCIImagePullSecrets(obj *unstructured.Unstructured, secretName string) error {
	imagePullSecrets, err := webhookutils.GetOrCreateNestedSlice(obj.Object, IsvcConfigs.ImagePullSecretPath...)
	if err != nil {
		return fmt.Errorf("failed to get spec.predictor.imagePullSecrets: %w", err)
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

	return webhookutils.SetNestedValue(obj.Object, imagePullSecrets, IsvcConfigs.ImagePullSecretPath)
}

// injectURIStorageUri injects storageUri into spec.predictor.model.storageUri for URI connections.
func (w *ConnectionWebhook) injectURIStorageUri(ctx context.Context, obj *unstructured.Unstructured, secretName, namespace string) error {
	// Fetch the secret to get the URI data
	secret := &corev1.Secret{}
	if err := w.APIReader.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	var storageUri string
	uri, exists := secret.Data["URI"]
	if !exists {
		return errors.New("secret does not contain 'URI' data key")
	}
	// The secret data is already base64 decoded by Kubernetes, so we can use it directly
	storageUri = string(uri)

	// Set the storageUri directly
	return webhookutils.SetNestedValue(obj.Object, storageUri, IsvcConfigs.StorageUriPath)
}

// injectS3StorageKey injects storage key into spec.predictor.model.storage.key for S3 connections.
func (w *ConnectionWebhook) injectS3StorageKey(obj *unstructured.Unstructured, secretName string) error {
	model, found, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return fmt.Errorf("failed to get spec.predictor.model: %w", err)
	}
	if !found {
		return errors.New("found no spec.predictor.model set in resource")
	}

	storageMap, err := webhookutils.GetOrCreateNestedMap(model, "storage")
	if err != nil {
		return fmt.Errorf("failed to get or create nested map for storage: %w", err)
	}
	storageMap["key"] = secretName
	model["storage"] = storageMap

	return webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath)
}
