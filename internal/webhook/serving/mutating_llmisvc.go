//go:build !nowebhook

package serving

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

type LLMInferenceServingPath struct {
	UriPath                []string
	ServiceAccountNamePath []string
}

var LlmisvcConfigs = LLMInferenceServingPath{
	UriPath: []string{"spec", "model", "uri"}, // all different protocol: pvc:// oci:// hf:// and s3:// are all using the same
	// hf and s3 need ServiceAccount binding
	ServiceAccountNamePath: []string{"spec", "template", "serviceAccountName"},
}

//+kubebuilder:webhook:path=/platform-connection-llmisvc,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=llminferenceservices,verbs=create;update,versions=v1beta1,name=connection-llmisvc.opendatahub.io,sideEffects=NoneOnDryRun,admissionReviewVersions=v1
//nolint:lll

type LLMISVCConnectionWebhook struct {
	APIReader client.Reader
	Client    client.Client // used to create ServiceAccount
	Decoder   admission.Decoder
	Name      string
}

var _ admission.Handler = &LLMISVCConnectionWebhook{}

func (w *LLMISVCConnectionWebhook) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/platform-connection-llmisvc", &webhook.Admission{
		Handler:        w,
		LogConstructor: webhookutils.NewWebhookLogConstructor(w.Name),
	})
	return nil
}

func (w *LLMISVCConnectionWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

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
		// allowed connection types for connection validation on llmisvc.
		allowedTypes := []string{
			ConnectionTypeURI.String(),
		}

		// validate the connection annotation
		// - if has matched annoataion
		// - if annaotation has valid value as that secret exists in the same namespace(permission allowed)
		validationResp, action, secretName, connectionType := webhookutils.ValidateInferenceServiceConnectionAnnotation(ctx, w.Client, obj, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		// only proceed with injection if the annotation is valid
		// Handle different actions based on the ConnectionAction value
		switch action {
		case webhookutils.ConnectionActionInject:
			// create ServiceAccount first (skip if it is dry-run)
			isDryRun := req.DryRun != nil && *req.DryRun
			if !isDryRun {
				// user sercret .data.headers to create for ServiceAccount
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
				return w.createPatchResponse(req, obj)
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
				return w.createPatchResponse(req, obj)
			}

			return admission.Allowed(fmt.Sprintf("Connection cleanup done for %s in namespace %s", req.Kind.Kind, req.Namespace))

		case webhookutils.ConnectionActionNone:
			// No action needed
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, no action needed", req.Namespace, req.Kind.Kind))

		default:
			log.V(1).Info("Unknown", "action", action)
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, unknown action: %s", req.Namespace, req.Kind.Kind, action))
		}

	default:
		return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, no action needed", req.Namespace, req.Kind.Kind))
	}
}

// createPatchResponse creates a patch response from the modified object.
func (w *LLMISVCConnectionWebhook) createPatchResponse(req admission.Request, obj *unstructured.Unstructured) admission.Response {
	marshaledObj, err := json.Marshal(obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
}

func (w *LLMISVCConnectionWebhook) performConnectionInjection(
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
	case ConnectionTypeURI:
		if err := w.injectModelUri(ctx, decodedObj, secretName, req.Namespace); err != nil {
			return false, fmt.Errorf("failed to inject host to .spec.model.uri: %w", err)
		}
		log.V(1).Info("Successfully injected URI from secret", "secretName", secretName)
		return true, nil

	default: // this should not enter since ValidateConnectionAnnotation ensures valid types, but keep it for safety
		log.V(1).Info("Unknown connection type, skipping injection", "connectionType", connectionType)
		return false, nil
	}
}

func (w *LLMISVCConnectionWebhook) performConnectionCleanup(
	ctx context.Context,
	req admission.Request,
	decodedObj *unstructured.Unstructured,
) (bool, error) {
	log := logf.FromContext(ctx)
	cleanupPerformed := false

	log.V(1).Info("Performing connection cleanup for removed annotation", "name", req.Name, "namespace", req.Namespace)
	// remove ServiceAccountName injection
	if err := w.handleSA(decodedObj, ""); err != nil {
		log.Error(err, "Failed to cleanup ServiceAccountName")
		return cleanupPerformed, fmt.Errorf("failed to cleanup ServiceAccountName: %w", err)
	}
	cleanupPerformed = true // ServiceAccount cleanup was performed

	if hasLLMISVCUri(decodedObj) {
		if _, err := w.cleanupURIStorageUri(decodedObj); err != nil {
			log.Error(err, "Failed to cleanup URI storageUri", "name", req.Name, "namespace", req.Namespace)
			return false, fmt.Errorf("failed to cleanup URI storageUri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up URI storageUri", "name", req.Name, "namespace", req.Namespace)
	}

	return cleanupPerformed, nil
}

// injectModelUri injects URI/https-host into .spec.model.uri for URI type connections.
// see https://github.com/kserve/kserve/blob/master/pkg/credentials/https/https_secret.go
func (w *LLMISVCConnectionWebhook) injectModelUri(ctx context.Context, obj *unstructured.Unstructured, secretName, namespace string) error {
	// Fetch the secret data
	secret := &corev1.Secret{}
	if err := w.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	// get host from either "https-host" or "URI" key
	var uriHost []byte
	var exists bool

	if uriHost, exists = secret.Data["https-host"]; !exists {
		if uriHost, exists = secret.Data["URI"]; !exists {
			return errors.New("secret does not contain either in 'https-host' or 'URI' data key")
		}
	}

	return webhookutils.SetNestedValue(obj.Object, string(uriHost), LlmisvcConfigs.UriPath)
}

// hasLLMISVCUri checks if the object has URI field in spec.model.uri.
func hasLLMISVCUri(obj *unstructured.Unstructured) bool {
	_, found, err := unstructured.NestedString(obj.Object, LlmisvcConfigs.UriPath...)
	return err == nil && found
}

// cleanupURIStorageUri delete the uri field from spec.model.
// cannot just set to empty string, it will fail in validation.
// do not opt for RemoveNestedField because there is no return to indicate if remove worked.
func (w *LLMISVCConnectionWebhook) cleanupURIStorageUri(obj *unstructured.Unstructured) (bool, error) {
	model, _, err := unstructured.NestedMap(obj.Object, "spec", "model")
	if err != nil {
		return false, fmt.Errorf("failed to get spec.model: %w", err)
	}

	// Remove the uri field
	delete(model, "uri")

	err = webhookutils.SetNestedValue(obj.Object, model, []string{"spec", "model"})
	return true, err
}

// handleSA injects serviceaccount into spec.template.serviceAccountName.
// if saName is "", it is removal.
func (w *LLMISVCConnectionWebhook) handleSA(obj *unstructured.Unstructured, saName string) error {
	if saName == "" {
		// Remove the field entirely
		unstructured.RemoveNestedField(obj.Object, LlmisvcConfigs.ServiceAccountNamePath...)
		return nil
	}
	return webhookutils.SetNestedValue(obj.Object, saName, LlmisvcConfigs.ServiceAccountNamePath)
}
