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

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

//+kubebuilder:webhook:path=/platform-connection-llmisvc,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=llminferenceservices,verbs=create;update,versions=v1alpha1,name=connection-llmisvc.opendatahub.io,sideEffects=NoneOnDryRun,admissionReviewVersions=v1
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
		allowedTypes := map[string][]string{
			annotations.ConnectionTypeProtocol: {
				webhookutils.ConnectionTypeProtocolURI.String(), // this is going to work for both uri:// and hf://
				webhookutils.ConnectionTypeProtocolS3.String(),
				webhookutils.ConnectionTypeProtocolOCI.String(),
			},
			annotations.ConnectionTypeRef: {
				webhookutils.ConnectionTypeRefURI.String(), // this is going to work for both uri:// and hf://
				webhookutils.ConnectionTypeRefS3.String(),
				webhookutils.ConnectionTypeRefOCI.String(),
			},
		}
		validationResp, newConn := webhookutils.ValidateServingConnectionAnnotation(ctx, w.Webhook.APIReader, obj, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		var action webhookutils.ConnectionAction
		var oldConn webhookutils.ConnectionInfo

		if req.Operation == admissionv1.Update {
			// UPDATE, get old connection info to determine remove/replace.
			oldConn, err = w.Webhook.GetOldConnectionInfo(ctx, req)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}

			action = webhookutils.DetermineConnectionChangeAction(oldConn, newConn)
		} else { // CREATE, always inject
			action = webhookutils.ConnectionActionInject
		}

		switch action {
		case webhookutils.ConnectionActionInject:
			// Create ServiceAccount only for S3 connections in non-dry-run mode
			isDryRun := req.DryRun != nil && *req.DryRun
			if err := webhookutils.ServiceAccountCreation(ctx, w.Webhook.Client, newConn.SecretName, newConn.Type, req.Namespace, isDryRun); err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			// Perform injection for valid connection types
			injectionPerformed, err := w.performConnectionInjection(ctx, req, obj, newConn)
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
			cleanupPerformed, err := w.performConnectionCleanup(ctx, req, obj, oldConn)
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
			log.V(1).Info("Connection changed, performing replacement",
				"oldType", oldConn.Type, "newType", newConn.Type,
				"oldSecret", oldConn.SecretName, "newSecret", newConn.SecretName,
				"oldPath", oldConn.Path, "newPath", newConn.Path)

			cleanupPerformed, err := w.performConnectionCleanup(ctx, req, obj, oldConn)
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
			if err := webhookutils.ServiceAccountCreation(ctx, w.Webhook.Client, newConn.SecretName, newConn.Type, req.Namespace, isDryRun); err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}

			// inject the new connection type
			injectionPerformed, err := w.performConnectionInjection(ctx, req, obj, newConn)
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
	decodedObj *unstructured.Unstructured,
	connInfo webhookutils.ConnectionInfo,
) (bool, error) {
	log := logf.FromContext(ctx)
	var uriValue string

	// injection based on connection type
	switch connInfo.Type {
	case webhookutils.ConnectionTypeProtocolURI.String(), webhookutils.ConnectionTypeRefURI.String():
		// TODO: inject serviceaccount for hf://
		var err error
		uriValue, err = w.Webhook.GetURIValue(ctx, decodedObj, connInfo.SecretName, req.Namespace)
		if err != nil {
			return false, fmt.Errorf("failed to get URI value from secret %s: %w", connInfo.SecretName, err)
		}
		if uriValue == "" {
			log.V(1).Info("No connection type URI value, skipping injection", "connectionType", connInfo.Type)
			return false, nil // Nothing to inject
		}

	case webhookutils.ConnectionTypeProtocolOCI.String(), webhookutils.ConnectionTypeRefOCI.String():
		if err := w.Webhook.InjectOCIImagePullSecrets(decodedObj, LlmisvcConfigs.ImagePullSecretPath, connInfo.SecretName); err != nil {
			return false, fmt.Errorf("failed to inject OCI .spec.template.imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully injected OCI .spec.template.imagePullSecrets", "secretName", connInfo.SecretName)

		// TODO: inject .spec.model.uri
		// uriValue = webhookutils.GetOCIValue(decodedObj)
		return true, nil

	case webhookutils.ConnectionTypeProtocolS3.String(), webhookutils.ConnectionTypeRefS3.String():
		// inject ServiceAccount for S3 connections
		if err := w.Webhook.InjectServiceAccountName(decodedObj, LlmisvcConfigs.ServiceAccountNamePath, connInfo.SecretName+"-sa"); err != nil {
			return false, fmt.Errorf("failed to inject .spec.template.serviceAccountName: %w", err)
		}
		log.V(1).Info("Successfully injected .spec.template.serviceAccountName", "ServiceAccountName", connInfo.SecretName+"-sa")

		var err error
		uriValue, err = w.Webhook.BuildS3URI(ctx, connInfo, req.Namespace)
		if err != nil {
			return false, fmt.Errorf("failed to build S3 URI: %w", err)
		}

	default: // this should not enter since ValidateConnectionAnnotation ensures valid types, but keep it for safety
		log.V(1).Info("Unknown connection type, skipping injection", "connectionType", connInfo.Type)
		return false, nil
	}

	if err := w.injectModelUri(decodedObj, uriValue); err != nil {
		return false, fmt.Errorf("failed to inject .spec.model.uri: %w", err)
	}
	log.V(1).Info("Successfully injected .spec.model.uri", "secretName", connInfo.SecretName)
	return true, nil
}

func (w *LLMISVCConnectionWebhook) performConnectionCleanup(
	ctx context.Context,
	req admission.Request,
	decodedObj *unstructured.Unstructured,
	connInfo webhookutils.ConnectionInfo,
) (bool, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Performing connection cleanup for removed annotation", "name", req.Name, "namespace", req.Namespace, "oldConnectionType", connInfo.Type)

	cleanupPerformed := false

	switch connInfo.Type {
	case "":
		// no old connection-type-ref means we do not know what was the old secret used for
		// so we need do a full cleanup to ensure before injecting the new one
		// for oci:
		if connInfo.SecretName != "" {
			if err := w.Webhook.CleanupOCIImagePullSecrets(decodedObj, LlmisvcConfigs.ImagePullSecretPath, connInfo.SecretName); err != nil {
				return false, fmt.Errorf("failed to cleanup OCI .spec.template.imagePullSecrets: %w", err)
			}
			log.V(1).Info("Successfully cleaned up OCI .spec.template.imagePullSecrets", "secretName", connInfo.SecretName, "name", req.Name, "namespace", req.Namespace)
		} else {
			// If we don't know the secret name, remove the entire imagePullSecrets field as fallback
			unstructured.RemoveNestedField(decodedObj.Object, LlmisvcConfigs.ImagePullSecretPath...)
			log.V(1).Info("Successfully cleaned up entire .spec.template.imagePullSecrets field", "name", req.Name, "namespace", req.Namespace)
		}

		// for s3: clean up ServiceAccountName injection
		if err := w.Webhook.RemoveServiceAccountName(decodedObj, LlmisvcConfigs.ServiceAccountNamePath, connInfo.SecretName+"-sa"); err != nil {
			return false, fmt.Errorf("failed to cleanup .spec.template.serviceAccountName: %w", err)
		}
		cleanupPerformed = true

	case webhookutils.ConnectionTypeProtocolS3.String(), webhookutils.ConnectionTypeRefS3.String(): // TODO: we will need handle hf:// from ConnectionTypeURI
		// remove ServiceAccountName injection, if we need it in replacement, we will add it back later.
		if err := w.Webhook.RemoveServiceAccountName(decodedObj, LlmisvcConfigs.ServiceAccountNamePath, connInfo.SecretName+"-sa"); err != nil {
			return false, fmt.Errorf("failed to cleanup .spec.template.serviceAccountName: %w", err)
		}
		cleanupPerformed = true

	case webhookutils.ConnectionTypeProtocolOCI.String(), webhookutils.ConnectionTypeRefOCI.String():
		if connInfo.SecretName != "" {
			if err := w.Webhook.CleanupOCIImagePullSecrets(decodedObj, LlmisvcConfigs.ImagePullSecretPath, connInfo.SecretName); err != nil {
				return false, fmt.Errorf("failed to cleanup OCI .spec.template.imagePullSecrets: %w", err)
			}
			log.V(1).Info("Successfully cleaned up OCI .spec.template.imagePullSecrets", "secretName", connInfo.SecretName, "name", req.Name, "namespace", req.Namespace)
			cleanupPerformed = true
		} else {
			log.V(1).Info("No old secret name, skipping cleanup", "name", req.Name, "namespace", req.Namespace)
		}
	}

	// for uri, s3, and oci: clean up model URI
	if hasLLMISVCUri(decodedObj) {
		if _, err := w.cleanupModelUri(decodedObj); err != nil {
			return false, fmt.Errorf("failed to cleanup URI .spec.model.uri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up from .spec.model.uri", "name", req.Name, "namespace", req.Namespace)
		cleanupPerformed = true
	}

	return cleanupPerformed, nil
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

// cleanupModelUri removes the entire .spec.model object.
// Since uri is required for .spec.model, removing the uri means we need to remove .spec.model entirely.
func (w *LLMISVCConnectionWebhook) cleanupModelUri(obj *unstructured.Unstructured) (bool, error) {
	_, found, err := unstructured.NestedMap(obj.Object, "spec", "model")
	if err != nil {
		return false, fmt.Errorf("failed to get .spec.model: %w", err)
	}
	if !found {
		// No model found, nothing to clean up
		return false, nil
	}

	// Remove the entire model object since uri is required
	unstructured.RemoveNestedField(obj.Object, "spec", "model")
	return true, nil
}
