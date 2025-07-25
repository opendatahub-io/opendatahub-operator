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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

type InferenceServingPath struct {
	ModelPath           []string
	ImagePullSecretPath []string
}

var IsvcConfigs = InferenceServingPath{
	ModelPath:           []string{"spec", "predictor", "model"},            // used by S3
	ImagePullSecretPath: []string{"spec", "predictor", "imagePullSecrets"}, // used by OCI
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

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		// allowed connection types for connection validation on isvc.
		allowedTypes := []string{"uri-v1", "s3", "oci-v1"}

		// validate the connection annotation
		// - if has matched annoataion
		// - if annaotation has valid value as that secret exists in the same namespace(permission allowed)
		validationResp, shouldInject, secret, connectionType := webhookutils.ValidateConnectionAnnotation(ctx, w.Client, w.Decoder, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		// only proceed with injection if the annotation is valid and shouldInject is true
		if !shouldInject {
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, no injection needed", req.Namespace, req.Kind.Kind))
		}

		injectionPerformed, obj, err := w.performConnectionInjection(ctx, req, secret, connectionType)
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

	default:
		resp = admission.Allowed(fmt.Sprintf("Operation %s on %s allowed in namespace %s", req.Operation, req.Kind.Kind, req.Namespace))
	}
	return resp
}

func (w *ConnectionWebhook) performConnectionInjection(
	ctx context.Context,
	req admission.Request,
	secret *corev1.Secret,
	connectionType string,
) (bool, *unstructured.Unstructured, error) {
	log := logf.FromContext(ctx)

	obj := &unstructured.Unstructured{}
	if err := w.Decoder.Decode(req, obj); err != nil {
		return false, nil, fmt.Errorf("failed to decode InferenceService object: %w", err)
	}
	log.Info("Decoded InferenceService object", "connectionType", connectionType, "operation", req.Operation)

	// injection based on connection type
	switch connectionType {
	case "oci-v1":
		if err := w.injectOCIImagePullSecrets(obj, secret.Name); err != nil {
			return false, nil, fmt.Errorf("failed to inject OCI imagePullSecrets: %w", err)
		}
		log.Info("Successfully injected OCI imagePullSecrets", "secretName", secret.Name)
		return true, obj, nil

	case "uri-v1":
		if err := w.injectURIStorageUri(obj, secret); err != nil {
			return false, nil, fmt.Errorf("failed to inject URI storageUri: %w", err)
		}
		log.Info("Successfully injected URI storageUri from secret", "secretName", secret.Name)
		return true, obj, nil

	case "s3":
		if err := w.injectS3StorageKey(obj, secret.Name); err != nil {
			return false, nil, fmt.Errorf("failed to inject S3 storage.key: %w", err)
		}
		log.Info("Successfully injected S3 storage key", "secretName", secret.Name)
		return true, obj, nil

	default: // this should not enter, but keep it just for sanity check if allowedTypes is updated.
		log.Info("Unknown connection type, skipping injection", "connectionType", connectionType)
		return false, nil, nil
	}
}

// injectOCIImagePullSecrets injects imagePullSecrets into spec.predictor.imagePullSecrets for OCI connections.
func (w *ConnectionWebhook) injectOCIImagePullSecrets(obj *unstructured.Unstructured, secretName string) error {
	imagePullSecrets, found, err := unstructured.NestedSlice(obj.Object, IsvcConfigs.ImagePullSecretPath...)
	if err != nil {
		return fmt.Errorf("failed to get spec.predictor.imagePullSecrets: %w", err)
	}
	// did not have imagePullSecrets (upon CREATE), just set to the new secret
	if !found {
		imagePullSecrets = []interface{}{
			map[string]interface{}{
				"name": secretName,
			},
		}
		return unstructured.SetNestedSlice(obj.Object, imagePullSecrets, IsvcConfigs.ImagePullSecretPath...)
	}

	// if already some secrets(upon UPDATE), and the secret is already there, fast exit
	for _, secret := range imagePullSecrets {
		if secretMap, ok := secret.(map[string]interface{}); ok {
			if name, exists := secretMap["name"]; exists && name == secretName {
				return nil
			}
		}
	}

	// add new secret to the secrets(upon UPDATE)
	newImagePullSecret := map[string]interface{}{
		"name": secretName,
	}
	imagePullSecrets = append(imagePullSecrets, newImagePullSecret)

	return unstructured.SetNestedSlice(obj.Object, imagePullSecrets, IsvcConfigs.ImagePullSecretPath...)
}

// injectURIStorageUri injects storageUri into spec.predictor.model.storageUri for URI connections.
func (w *ConnectionWebhook) injectURIStorageUri(obj *unstructured.Unstructured, secret *corev1.Secret) error {
	var storageUri string
	if uri, exists := secret.Data["URI"]; exists {
		// The secret data is already base64 decoded by Kubernetes, so we can use it directly
		storageUri = string(uri)
	} else {
		return errors.New("secret does not contain 'URI' data key")
	}

	// Set the storageUri directly
	return unstructured.SetNestedField(obj.Object, storageUri, "spec", "predictor", "model", "storageUri")
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

	return unstructured.SetNestedMap(obj.Object, model, IsvcConfigs.ModelPath...)
}
