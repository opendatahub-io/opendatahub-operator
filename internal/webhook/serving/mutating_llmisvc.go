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
	UriPath []string
}

var LlmisvcConfigs = LLMInferenceServingPath{
	UriPath: []string{"spec", "model", "uri"}, // used by URI, has string
}

//+kubebuilder:webhook:path=/platform-connection-llmisvc,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=llminferenceservices,verbs=create;update,versions=v1beta1,name=connection-llmisvc.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

type LLMISVCConnectionWebhook struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
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

func (w *LLMISVCConnectionWebhook) performConnectionInjection(
	ctx context.Context,
	req admission.Request,
	secretName string,
	connectionType string,
	decodedObj *unstructured.Unstructured,
) (bool, error) {
	log := logf.FromContext(ctx)

	log.V(1).Info("Decoded LLMInferenceService object", "connectionType", connectionType, "operation", req.Operation)

	// injection based on connection type
	switch ConnectionType(connectionType) {
	case ConnectionTypeURI:
		if err := w.injectUri(ctx, decodedObj, secretName, req.Namespace); err != nil {
			return false, fmt.Errorf("failed to inject URI to .spec.model.uri: %w", err)
		}
		log.V(1).Info("Successfully injected URI from secret", "secretName", secretName)
		return true, nil

	default: // this should not enter since ValidateConnectionAnnotation ensures valid types, but keep it for safety
		log.V(1).Info("Unknown connection type, skipping injection", "connectionType", connectionType)
		return false, nil
	}
}

// injectUri injects URL into spec.model.uri for URI type connections.
func (w *LLMISVCConnectionWebhook) injectUri(ctx context.Context, obj *unstructured.Unstructured, secretName, namespace string) error {
	// Fetch the secret to get the URI data
	secret := &corev1.Secret{}
	if err := w.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	uri, exists := secret.Data["URI"]
	if !exists {
		return errors.New("secret does not contain 'URI' data key")
	}

	return webhookutils.SetNestedValue(obj.Object, string(uri), LlmisvcConfigs.UriPath)
}
