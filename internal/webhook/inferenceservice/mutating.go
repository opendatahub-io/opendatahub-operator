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
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

// create new type for connection types.

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

	// Perform prechecks
	if resp := w.precheck(ctx, req); resp != nil {
		return *resp
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
			webhookutils.ConnectionTypeURI.String(),
			webhookutils.ConnectionTypeS3.String(),
			webhookutils.ConnectionTypeOCI.String(),
		}

		// validate the connection annotation and get secret and type, actual action (create/injenct, remove, replace) is moved out of here.
		validationResp, secretName, connectionType := webhookutils.ValidateInferenceServiceConnectionAnnotation(ctx, w.APIReader, obj, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		var action webhookutils.ConnectionAction
		var oldSecretName, oldConnectionType string

		if req.Operation == admissionv1.Update {
			// UPDATE, get old connection type and secret name to determine remove replace.
			oldSecretName, oldConnectionType, err = w.getOldConnectionInfo(ctx, req)
			if err != nil {
				log.Error(err, "Failed to get old connection info")
				return admission.Errored(http.StatusInternalServerError, err)
			}
			action = webhookutils.DetermineConnectionChangeAction(oldSecretName, oldConnectionType, secretName, connectionType)
		} else { // CREATE, always inject
			action = webhookutils.ConnectionActionInject
		}

		// Handle different actions based on the action
		switch action {
		case webhookutils.ConnectionActionInject:
			// Create ServiceAccount only for S3 connections in non-dry-run mode
			isDryRun := req.DryRun != nil && *req.DryRun
			if err := webhookutils.HandleServiceAccountCreation(ctx, w.Client, secretName, connectionType, req.Namespace, isDryRun); err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
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
			cleanupPerformed, err := w.performConnectionCleanup(ctx, req, obj, oldConnectionType)
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

			return admission.Allowed(fmt.Sprintf("Connection cleanup not needed for %s in namespace %s", req.Kind.Kind, req.Namespace))

		case webhookutils.ConnectionActionReplace:
			// Connection changed cleanup old and inject new
			log.V(1).Info("Connection type/secret changed, performing replacement",
				"oldType", oldConnectionType, "newType", connectionType,
				"oldSecret", oldSecretName, "newSecret", secretName)

			var cleanupPerformed bool
			cleanupPerformed, err = w.performConnectionCleanup(ctx, req, obj, oldConnectionType)
			if err != nil {
				log.Error(err, "Failed to cleanup old connection type")
				return admission.Errored(http.StatusInternalServerError, err)
			}

			// cleanupPerformed is always true if no error occurred
			if cleanupPerformed {
				log.V(1).Info("Successfully cleaned up old connection type")
			}
			// Create ServiceAccount only for S3 connections in non-dry-run mode
			isDryRun := req.DryRun != nil && *req.DryRun
			if err := webhookutils.HandleServiceAccountCreation(ctx, w.Client, secretName, connectionType, req.Namespace, isDryRun); err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}

			// inject the new connection type
			injectionPerformed, err := w.performConnectionInjection(ctx, req, secretName, connectionType, obj)
			if err != nil {
				log.Error(err, "Failed to inject new connection type")
				return admission.Errored(http.StatusInternalServerError, err)
			}

			// Write updated object back to k8s if any changes were made (cleanup or injection)
			if cleanupPerformed || injectionPerformed {
				marshaledObj, err := json.Marshal(obj)
				if err != nil {
					return admission.Errored(http.StatusInternalServerError, err)
				}
				return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
			}

			return admission.Allowed(fmt.Sprintf("No connection changes performed for %s in namespace %s", req.Kind.Kind, req.Namespace))

		case webhookutils.ConnectionActionNone:
			// No change needed
			return admission.Allowed(fmt.Sprintf("No connection change needed for %s in namespace %s", req.Kind.Kind, req.Namespace))

		default:
			log.V(1).Info("Unknown connection action", "action", action)
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, unknown action: %s", req.Namespace, req.Kind.Kind, action))
		}

	default: // Delete, Connection operation
		return admission.Allowed(fmt.Sprintf("Operation %s on %s allowed in namespace %s", req.Operation, req.Kind.Kind, req.Namespace))
	}
}

// precheck validates webhook initialization and request object.
func (w *ConnectionWebhook) precheck(ctx context.Context, req admission.Request) *admission.Response {
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

func (w *ConnectionWebhook) performConnectionInjection(
	ctx context.Context,
	req admission.Request,
	secretName string,
	connectionType string,
	decodedObj *unstructured.Unstructured,
) (bool, error) {
	log := logf.FromContext(ctx)

	// injection based on connection type
	switch connectionType {
	case webhookutils.ConnectionTypeOCI.String():
		if err := w.injectOCIImagePullSecrets(decodedObj, secretName); err != nil {
			return false, fmt.Errorf("failed to inject OCI imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully injected OCI imagePullSecrets", "secretName", secretName)
		return true, nil

	case webhookutils.ConnectionTypeURI.String():
		if err := w.injectURIStorageUri(ctx, decodedObj, secretName, req.Namespace); err != nil {
			return false, fmt.Errorf("failed to inject URI storageUri: %w", err)
		}
		log.V(1).Info("Successfully injected URI storageUri from secret", "secretName", secretName)
		return true, nil

	case webhookutils.ConnectionTypeS3.String():
		// inject ServiceAccount only for S3 connections
		if err := w.handleSA(decodedObj, secretName+"-sa"); err != nil {
			log.Error(err, "Failed to inject ServiceAccount")
			return false, fmt.Errorf("failed to inject ServiceAccount: %w", err)
		}
		log.V(1).Info("Successfully injected ServiceAccount", "ServiceAccountName", secretName+"-sa")
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
// Uses oldConnectionType to determine exactly what needs to be cleaned up.
func (w *ConnectionWebhook) performConnectionCleanup(
	ctx context.Context,
	req admission.Request,
	decodedObj *unstructured.Unstructured,
	oldConnectionType string,
) (bool, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Performing connection cleanup for removed annotation", "name", req.Name, "namespace", req.Namespace, "oldConnectionType", oldConnectionType)

	cleanupPerformed := true

	// Clean up based on the old connection type or just try cleanup all.
	switch oldConnectionType {
	case "":
		// no old connection type means we do not know what was there so we need do a full cleanup
		// to ensure before injecting the new one
		if err := w.cleanupOCIImagePullSecrets(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup OCI imagePullSecrets", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup OCI imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully cleaned up OCI imagePullSecrets", "name", req.Name, "namespace", req.Namespace)

		if err := w.cleanupURIStorageUri(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup URI storageUri", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup URI storageUri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up URI storageUri", "name", req.Name, "namespace", req.Namespace)

		// Clean up ServiceAccountName injection
		if err := w.handleSA(decodedObj, ""); err != nil {
			log.Error(err, "Failed to cleanup ServiceAccountName")
			return false, fmt.Errorf("failed to cleanup ServiceAccountName: %w", err)
		}

		if err := w.cleanupS3StorageKey(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup S3 storage key", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup S3 storage key: %w", err)
		}
		log.V(1).Info("Successfully cleaned up S3 storage key", "name", req.Name, "namespace", req.Namespace)

	case webhookutils.ConnectionTypeOCI.String():
		if err := w.cleanupOCIImagePullSecrets(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup OCI imagePullSecrets", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup OCI imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully cleaned up OCI imagePullSecrets", "name", req.Name, "namespace", req.Namespace)

	case webhookutils.ConnectionTypeURI.String():
		if err := w.cleanupURIStorageUri(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup URI storageUri", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup URI storageUri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up URI storageUri", "name", req.Name, "namespace", req.Namespace)

	case webhookutils.ConnectionTypeS3.String():
		// remove ServiceAccountName injection, if we need it in replacement, we ill add it back later.
		if err := w.handleSA(decodedObj, ""); err != nil {
			log.Error(err, "Failed to cleanup ServiceAccountName")
			return false, fmt.Errorf("failed to cleanup ServiceAccountName: %w", err)
		}
		if err := w.cleanupS3StorageKey(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup S3 storage key", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup S3 storage key: %w", err)
		}
		log.V(1).Info("Successfully cleaned up S3 storage key", "name", req.Name, "namespace", req.Namespace)

	default:
		// No specific cleanup needed for unknown connection types
		log.V(1).Info("No specific cleanup needed for connection type", "connectionType", oldConnectionType)
		cleanupPerformed = false
	}

	return cleanupPerformed, nil
}

// cleanupOCIImagePullSecrets set empty slice to spec.predictor.imagePullSecrets.
func (w *ConnectionWebhook) cleanupOCIImagePullSecrets(obj *unstructured.Unstructured) error {
	err := webhookutils.SetNestedValue(obj.Object, []interface{}{}, IsvcConfigs.ImagePullSecretPath)
	return err
}

// cleanupURIStorageUri delete the storageUri field from spec.predictor.model.
// cannot just set to empty string, it will fail in ValidateStorageURI().
func (w *ConnectionWebhook) cleanupURIStorageUri(obj *unstructured.Unstructured) error {
	model, found, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return fmt.Errorf("failed to get spec.predictor.model: %w", err)
	}
	if !found {
		return nil
	}

	// Remove the storageUri field
	delete(model, "storageUri")

	return webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath)
}

// cleanupS3StorageKey removes the storage field from spec.predictor.model.
func (w *ConnectionWebhook) cleanupS3StorageKey(obj *unstructured.Unstructured) error {
	model, found, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return fmt.Errorf("failed to get spec.predictor.model: %w", err)
	}
	if !found {
		return nil
	}

	// Remove the storage field
	delete(model, "storage")

	return webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath)
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

// getOldConnectionInfo extracts connection information from the old inferenceservice object.
// for UPDATE operations to determine if connection type has changed.
func (w *ConnectionWebhook) getOldConnectionInfo(ctx context.Context, req admission.Request) (string, string, error) {
	log := logf.FromContext(ctx)

	// Decode the old object
	oldObj := &unstructured.Unstructured{}
	if err := w.Decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
		log.Error(err, "failed to decode old inferenceservice object")
		return "", "", fmt.Errorf("failed to decode old inferenceservice object: %w", err)
	}

	// Get old annotation value
	oldAnnotationValue := resources.GetAnnotation(oldObj, annotations.Connection)
	if oldAnnotationValue == "" {
		return "", "", nil // No old connection
	}

	// Get old connection type from the secret
	secretMeta := resources.GvkToPartial(gvk.Secret)
	if err := w.APIReader.Get(ctx, types.NamespacedName{Name: oldAnnotationValue, Namespace: req.Namespace}, secretMeta); err != nil {
		if k8serr.IsNotFound(err) {
			log.V(1).Info("Old secret not found, treating as no old connection", "secretName", oldAnnotationValue)
			return "", "", nil
		}
		return "", "", fmt.Errorf("failed to get old secret metadata: %w", err)
	}

	oldConnectionType := resources.GetAnnotation(secretMeta, annotations.ConnectionTypeRef)
	return oldAnnotationValue, oldConnectionType, nil
}
