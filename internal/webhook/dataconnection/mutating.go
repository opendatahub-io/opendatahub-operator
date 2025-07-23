//go:build !nowebhook

package dataconnection

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

//+kubebuilder:webhook:path=/platform-dataconnection-isvc,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=inferenceservices,verbs=create;update,versions=v1beta1,name=dataconnection-isvc.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

type DataConnectionWebhook struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
}

var _ admission.Handler = &DataConnectionWebhook{}

func (w *DataConnectionWebhook) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/platform-dataconnection-isvc", &webhook.Admission{
		Handler:        w,
		LogConstructor: webhookutils.NewWebhookLogConstructor(w.Name),
	})
	return nil
}

func (w *DataConnectionWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	if w.Decoder == nil {
		log.Error(nil, "Decoder is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		// Define allowed connection types for data connection validation on isvc.
		allowedTypes := []string{"uri-v1", "s3", "oci-v1"}

		// validate the data connection annotation
		// - if has matched annoataion
		// - if annaotation has valid value as that secret is in the same namespace(permission allowed)
		validationResp, shouldInject, secret, connectionType := webhookutils.ValidateDataConnectionAnnotation(ctx, w.Client, w.Decoder, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		// only proceed with injection if the annotation is valid and shouldInject is true
		if !shouldInject {
			return admission.Allowed(fmt.Sprintf("Data connection validation passed in namespace %s for %s, no injection needed", req.Namespace, req.Kind.Kind))
		}

		injectionPerformed, obj, err := w.performDataConnectionInjection(ctx, req, secret, connectionType)
		if err != nil {
			log.Error(err, "Failed to perform data connection injection")
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if injectionPerformed {
			marshaledObj, err := json.Marshal(obj)
			if err != nil {
				log.Error(err, "Failed to marshal modified object")
				return admission.Errored(http.StatusInternalServerError, err)
			}
			return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		}

	default:
		resp = admission.Allowed(fmt.Sprintf("Operation %s on %s allowed in namespace %s", req.Operation, req.Kind.Kind, req.Namespace))
	}
	return resp
}

func (w *DataConnectionWebhook) performDataConnectionInjection(
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
			return false, nil, fmt.Errorf("failed to inject S3 storage key: %w", err)
		}
		log.Info("Successfully injected S3 storage key", "secretName", secret.Name)
		return true, obj, nil

	default: // this should not enter, but keep it just for sanity check if allowedTypes is updated.
		log.Info("Unknown connection type, skipping injection", "connectionType", connectionType)
		return false, nil, nil
	}
}

// injectOCIImagePullSecrets injects imagePullSecrets into spec.predictor.imagePullSecrets for OCI connections.
func (w *DataConnectionWebhook) injectOCIImagePullSecrets(obj *unstructured.Unstructured, secretName string) error {
	imagePullSecret := map[string]interface{}{
		"name": secretName,
	}

	// Get existing imagePullSecrets or create new slice
	imagePullSecrets, found, err := unstructured.NestedSlice(obj.Object, "spec", "predictor", "imagePullSecrets")
	if err != nil {
		return fmt.Errorf("failed to get imagePullSecrets: %w", err)
	}
	if !found {
		imagePullSecrets = make([]interface{}, 0)
	}

	// if the secret is already in the list (upon UPDATE), then skip adding it
	for _, secret := range imagePullSecrets {
		if secretMap, ok := secret.(map[string]interface{}); ok {
			if name, exists := secretMap["name"]; exists && name == secretName {
				return nil
			}
		}
	}

	// Add as new imagePullSecret
	imagePullSecrets = append(imagePullSecrets, imagePullSecret)

	return unstructured.SetNestedSlice(obj.Object, imagePullSecrets, "spec", "predictor", "imagePullSecrets")
}

// injectURIStorageUri injects storageUri into spec.predictor.model.storageUri for URI connections.
func (w *DataConnectionWebhook) injectURIStorageUri(obj *unstructured.Unstructured, secret *corev1.Secret) error {
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
func (w *DataConnectionWebhook) injectS3StorageKey(obj *unstructured.Unstructured, secretName string) error {
	// can be no storage, or can be with storage.key but need updated
	storage, found, err := unstructured.NestedMap(obj.Object, "spec", "predictor", "model", "storage")
	if err != nil || !found {
		storage = make(map[string]interface{})
	}

	// Set the key
	storage["key"] = secretName

	// Set the storage field back
	return unstructured.SetNestedMap(obj.Object, storage, "spec", "predictor", "model", "storage")
}
