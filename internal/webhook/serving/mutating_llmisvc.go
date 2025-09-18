//go:build !nowebhook

package serving

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

type LLMInferenceServingPath struct {
	UriPath                []string
	ImagePullSecretPath    []string
	ServiceAccountNamePath []string
}

var LlmisvcConfigs = LLMInferenceServingPath{
	UriPath:             []string{"spec", "model", "uri"}, // all different protocol: pvc:// oci:// hf:// and s3:// are all using the same
	ImagePullSecretPath: []string{"spec", "template", "imagePullSecrets"},
	// hf and s3 need ServiceAccount binding
	ServiceAccountNamePath: []string{"spec", "template", "serviceAccountName"},
}

//+kubebuilder:webhook:path=/platform-connection-llmisvc,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=llminferenceservices,verbs=create;update,versions=v1beta1,name=connection-llmisvc.opendatahub.io,sideEffects=NoneOnDryRun,admissionReviewVersions=v1
//nolint:lll

type LLMISVCConnectionWebhook struct {
	Webhook webhookutils.BaseServingConnectionWebhook
}

var _ admission.Handler = &LLMISVCConnectionWebhook{}

func (w *LLMISVCConnectionWebhook) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/platform-connection-llmisvc", &webhook.Admission{
		Handler:        w,
		LogConstructor: webhookutils.NewWebhookLogConstructor(w.Webhook.Name),
	})
	return nil
}

func (w *LLMISVCConnectionWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Perform prechecks
	if resp := w.Webhook.WebhookPrecheck(ctx, req); resp != nil {
		return *resp
	}
	// Decode the object once
	obj, err := webhookutils.DecodeUnstructured(w.Webhook.Decoder, req)
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
			webhookutils.ConnectionTypeURI.String(), // this is going to work for both uri:// and hf://
			webhookutils.ConnectionTypeS3.String(),
			webhookutils.ConnectionTypeOCI.String(),
		}

		validationResp, secretName, connectionType := webhookutils.ValidateServingConnectionAnnotation(ctx, w.Webhook.APIReader, obj, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		var action webhookutils.ConnectionAction
		var oldSecretName, oldConnectionType string

		if req.Operation == admissionv1.Update {
			// UPDATE, get old connection type and secret name to determine remove replace.
			oldSecretName, oldConnectionType, err = w.Webhook.GetOldConnectionInfo(ctx, req)
			if err != nil {
				log.Error(err, "Failed to get old connection info")
				return admission.Errored(http.StatusInternalServerError, err)
			}
			action = webhookutils.DetermineConnectionChangeAction(oldSecretName, oldConnectionType, secretName, connectionType)
		} else { // CREATE, always inject
			action = webhookutils.ConnectionActionInject
		}

		switch action {
		case webhookutils.ConnectionActionInject:
			// Create ServiceAccount only for S3 connections in non-dry-run mode
			isDryRun := req.DryRun != nil && *req.DryRun
			if err := webhookutils.ServiceAccountCreation(ctx, w.Webhook.Client, secretName, connectionType, req.Namespace, isDryRun); err != nil {
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
				return w.Webhook.CreatePatchResponse(req, obj)
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
				return w.Webhook.CreatePatchResponse(req, obj)
			}

			return admission.Allowed(fmt.Sprintf("Connection cleanup not needed for %s in namespace %s", req.Kind.Kind, req.Namespace))

		case webhookutils.ConnectionActionReplace:
			// Connection changed cleanup old and inject new
			log.V(1).Info("Connection type/secret changed, performing replacement",
				"oldType", oldConnectionType, "newType", connectionType,
				"oldSecret", oldSecretName, "newSecret", secretName)

			cleanupPerformed, err := w.performConnectionCleanup(ctx, req, obj, oldConnectionType)
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
			if err := webhookutils.ServiceAccountCreation(ctx, w.Webhook.Client, secretName, connectionType, req.Namespace, isDryRun); err != nil {
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
				return w.Webhook.CreatePatchResponse(req, obj)
			}

			return admission.Allowed(fmt.Sprintf("No connection changes performed for %s in namespace %s", req.Kind.Kind, req.Namespace))

		case webhookutils.ConnectionActionNone:
			// No action needed
			return admission.Allowed(fmt.Sprintf("No connection change needed for %s in namespace %s", req.Kind.Kind, req.Namespace))

		default:
			log.V(1).Info("Unknown connection action", "action", action)
			return admission.Allowed(fmt.Sprintf("Connection validation passed in namespace %s for %s, unknown action: %s", req.Namespace, req.Kind.Kind, action))
		}

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
	var uriValue string

	// injection based on connection type
	switch connectionType {
	case webhookutils.ConnectionTypeURI.String():
		// TODO: inject serviceaccount for hf://
		var err error
		uriValue, err = w.Webhook.GetURIValue(ctx, decodedObj, secretName, req.Namespace)
		if err != nil {
			return false, fmt.Errorf("failed to get URI value: %w", err)
		}
		if uriValue == "" {
			log.V(1).Info("No connection type URI value, skipping injection", "connectionType", connectionType)
			return false, nil // Nothing to inject
		}

	case webhookutils.ConnectionTypeOCI.String():
		if err := w.Webhook.InjectOCIImagePullSecrets(decodedObj, LlmisvcConfigs.ImagePullSecretPath, secretName); err != nil {
			return false, fmt.Errorf("failed to inject OCI .spec.template.imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully injected OCI .spec.template.imagePullSecrets", "secretName", secretName)

		// TODO: inject .spec.model.uri
		// uriValue = webhookutils.GetOCIValue(decodedObj)
		return true, nil

	case webhookutils.ConnectionTypeS3.String():
		// inject ServiceAccount for S3 connections
		if err := w.Webhook.HandleSA(decodedObj, LlmisvcConfigs.ServiceAccountNamePath, secretName+"-sa"); err != nil {
			return false, fmt.Errorf("failed to inject .spec.template.serviceAccountName: %w", err)
		}
		log.V(1).Info("Successfully injected .spec.template.serviceAccountName", "ServiceAccountName", secretName+"-sa")

		var err error
		uriValue, err = w.Webhook.GetS3Value(ctx, decodedObj, secretName, req.Namespace)
		if err != nil {
			return false, fmt.Errorf("failed to get S3 URI value: %w", err)
		}
		if uriValue == "" {
			log.V(1).Info("No S3 URI value, skipping injection", "connectionType", connectionType)
			return false, nil // Nothing to inject
		}

	default: // this should not enter since ValidateConnectionAnnotation ensures valid types, but keep it for safety
		log.V(1).Info("Unknown connection type, skipping injection", "connectionType", connectionType)
		return false, nil
	}

	if err := w.injectModelUri(decodedObj, uriValue); err != nil {
		return false, fmt.Errorf("failed to inject .spec.model.uri: %w", err)
	}
	log.V(1).Info("Successfully injected .spec.model.uri", "secretName", secretName)
	return true, nil
}

func (w *LLMISVCConnectionWebhook) performConnectionCleanup(
	ctx context.Context,
	req admission.Request,
	decodedObj *unstructured.Unstructured,
	oldConnectionType string,
) (bool, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Performing connection cleanup for removed annotation", "name", req.Name, "namespace", req.Namespace, "oldConnectionType", oldConnectionType)

	if oldConnectionType == webhookutils.ConnectionTypeS3.String() { // TODO: we will need handl hf://
		// remove ServiceAccountName injection, if we need it in replacement, we ill add it back later.
		if err := w.Webhook.HandleSA(decodedObj, LlmisvcConfigs.ServiceAccountNamePath, ""); err != nil {
			return false, fmt.Errorf("failed to cleanup .spec.template.serviceAccountName: %w", err)
		}
	}

	if hasLLMISVCUri(decodedObj) {
		if _, err := w.cleanupURIStorageUri(decodedObj); err != nil {
			return false, fmt.Errorf("failed to cleanup URI .spec.model.uri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up from .spec.model.uri", "name", req.Name, "namespace", req.Namespace)
		return true, nil
	}

	return false, nil
}

// injectModelUri injects URI value into spec.model.uri for LLMISVC connections.
// This function takes a URI value and sets it in the object, creating the spec.model
// structure if it doesn't exist.
func (w *LLMISVCConnectionWebhook) injectModelUri(obj *unstructured.Unstructured, uriValue string) error {
	// Get the spec object - it should always exist after CRD validation
	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found {
		return fmt.Errorf("failed to get .spec: %w", err)
	}

	// Get or create the model object within spec
	model, err := webhookutils.GetOrCreateNestedMap(spec, "model")
	if err != nil {
		return fmt.Errorf("failed to get or create .spec.model: %w", err)
	}

	// Set the uri field in the model object
	model["uri"] = uriValue

	// Set the updated model back to spec
	spec["model"] = model

	// Set the updated spec back to the object
	if err := webhookutils.SetNestedValue(obj.Object, spec, []string{"spec"}); err != nil {
		return fmt.Errorf("failed to set .spec: %w", err)
	}
	return nil
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
	model, found, err := unstructured.NestedMap(obj.Object, "spec", "model")
	if err != nil {
		return false, fmt.Errorf("failed to get .spec.model: %w", err)
	}
	if !found {
		// No model found, nothing to clean up
		return false, nil
	}

	// Remove the uri field
	delete(model, "uri")

	if err := webhookutils.SetNestedValue(obj.Object, model, []string{"spec", "model"}); err != nil {
		return false, fmt.Errorf("failed to set .spec.model after cleanup: %w", err)
	}
	return true, nil
}
