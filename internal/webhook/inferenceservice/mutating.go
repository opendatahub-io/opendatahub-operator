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
	ModelPath           []string
	ImagePullSecretPath []string
	StorageUriPath      []string
}

var IsvcConfigs = InferenceServingPath{
	ModelPath:           []string{"spec", "predictor", "model"},               // used by S3, has map
	ImagePullSecretPath: []string{"spec", "predictor", "imagePullSecrets"},    // used by OCI, has slice
	StorageUriPath:      []string{"spec", "predictor", "model", "storageUri"}, // used by URI, has string
}

//+kubebuilder:webhook:path=/platform-connection-isvc,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=inferenceservices,verbs=create;update,versions=v1beta1,name=connection-isvc.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

type ConnectionWebhook struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
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

		// validate the connection annotation
		// - if has matched annoataion
		// - if annaotation has valid value as that secret exists in the same namespace(permission allowed)
		validationResp, shouldInject, secretName, connectionType := webhookutils.ValidateInferenceServiceConnectionAnnotation(ctx, w.Client, obj, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		// only proceed with injection if the annotation is valid and shouldInject is true
		if !shouldInject {
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, no injection needed", req.Namespace, req.Kind.Kind))
		}

		injectionPerformed, err := w.performConnectionInjection(ctx, req, secretName, connectionType, obj)
		if err != nil {
			log.Error(err, "Failed to perform connection injection")
			return admission.Errored(http.StatusInternalServerError, err)
		}

		// finally, write updated object back to k8s
		if injectionPerformed {
			marshaledObj, err := json.Marshal(obj)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		}

		// If no injection was performed, allow the operation
		return admission.Allowed(fmt.Sprintf("No injection performed for %s in namespace %s", req.Kind.Kind, req.Namespace))

	default:
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

	log.V(1).Info("Decoded InferenceService object", "connectionType", connectionType, "operation", req.Operation)

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
	if err := w.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
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
