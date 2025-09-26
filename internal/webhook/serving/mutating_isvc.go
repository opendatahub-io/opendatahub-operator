//go:build !nowebhook

package serving

import (
	"context"
	"errors"
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

type ISVCConnectionWebhook struct {
	Webhook webhookutils.BaseServingConnectionWebhook
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

var _ admission.Handler = &ISVCConnectionWebhook{}

func (w *ISVCConnectionWebhook) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/platform-connection-isvc", &webhook.Admission{
		Handler:        w,
		LogConstructor: webhookutils.NewWebhookLogConstructor(w.Webhook.Name),
	})
	return nil
}

func (w *ISVCConnectionWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
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
		// allowed connection types for connection validation on isvc.
		allowedTypes := map[string][]string{
			annotations.ConnectionTypeProtocol: {
				webhookutils.ConnectionTypeProtocolURI.String(),
				webhookutils.ConnectionTypeProtocolS3.String(),
				webhookutils.ConnectionTypeProtocolOCI.String(),
			},
			annotations.ConnectionTypeRef: {
				webhookutils.ConnectionTypeRefURI.String(),
				webhookutils.ConnectionTypeRefS3.String(),
				webhookutils.ConnectionTypeRefOCI.String(),
			},
		}

		// validate the connection annotation and get secret and type, actual action (create/injenct, remove, replace) is moved out of here.
		validationResp, newConn := webhookutils.ValidateServingConnectionAnnotation(ctx, w.Webhook.APIReader, obj, req, allowedTypes)
		if !validationResp.Allowed {
			return validationResp
		}

		var action webhookutils.ConnectionAction
		var oldConn webhookutils.ConnectionInfo

		if req.Operation == admissionv1.Update {
			// UPDATE, get old connection info to determine delete/update/none.
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
			log.V(1).Info("Connection info changed, performing replacement",
				"oldType", oldConn.Type, "newType", newConn.Type,
				"oldSecret", oldConn.SecretName, "newSecret", newConn.SecretName,
				"oldPath", oldConn.Path, "newPath", newConn.Path)

			var cleanupPerformed bool
			cleanupPerformed, err = w.performConnectionCleanup(ctx, req, obj, oldConn)
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

func (w *ISVCConnectionWebhook) performConnectionInjection(
	ctx context.Context,
	req admission.Request,
	decodedObj *unstructured.Unstructured,
	connInfo webhookutils.ConnectionInfo,
) (bool, error) {
	log := logf.FromContext(ctx)

	// injection based on connection type
	switch connInfo.Type {
	case webhookutils.ConnectionTypeProtocolOCI.String(), webhookutils.ConnectionTypeRefOCI.String():
		if err := w.Webhook.InjectOCIImagePullSecrets(decodedObj, IsvcConfigs.ImagePullSecretPath, connInfo.SecretName); err != nil {
			return false, fmt.Errorf("failed to inject OCI .spec.predictor.imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully injected OCI .spec.predictor.imagePullSecrets", "secretName", connInfo.SecretName)
		// TODO: inject .spec.model.uri
		return true, nil

	case webhookutils.ConnectionTypeProtocolURI.String(), webhookutils.ConnectionTypeRefURI.String():
		if err := w.injectURIStorageUri(ctx, decodedObj, connInfo.SecretName, req.Namespace); err != nil {
			return false, fmt.Errorf("failed to inject host to .spec.predictor.model.storageUri: %w", err)
		}
		log.V(1).Info("Successfully injected URI .spec.predictor.model.storageUri", "secretName", connInfo.SecretName)
		return true, nil

	case webhookutils.ConnectionTypeProtocolS3.String(), webhookutils.ConnectionTypeRefS3.String():
		// inject ServiceAccount only for S3 connections
		if err := w.Webhook.HandleSA(decodedObj, IsvcConfigs.ServiceAccountNamePath, connInfo.SecretName+"-sa"); err != nil {
			return false, fmt.Errorf("failed to inject .spec.predictor.serviceAccountName: %w", err)
		}
		log.V(1).Info("Successfully injected .spec.predictor.serviceAccountName", "ServiceAccountName", connInfo.SecretName+"-sa")
		// get old .spec.predictor.model.storage.path
		var oldSpecPath string
		if req.Operation == admissionv1.Update && req.OldObject.Raw != nil {
			oldObj := &unstructured.Unstructured{}
			if err := w.Webhook.Decoder.DecodeRaw(req.OldObject, oldObj); err == nil {
				if oldStoragePath, found, _ := unstructured.NestedString(oldObj.Object, "spec", "predictor", "model", "storage", "path"); found {
					oldSpecPath = oldStoragePath
				}
			}
		}

		if err := w.injectS3StorageKeyPath(decodedObj, connInfo, oldSpecPath); err != nil {
			return false, fmt.Errorf("failed to inject S3 .spec.predictor.model.storage: %w", err)
		}
		log.V(1).Info("Successfully injected S3 .spec.predictor.model.storage", "secretName", connInfo.SecretName)
		return true, nil

	default: // this should not enter since ValidateConnectionAnnotation ensures valid types, but keep it for safety
		log.V(1).Info("Unknown connection type, skipping injection", "connectionType", connInfo.Type)
		return false, nil
	}
}

// performConnectionCleanup removes previously injected connection fields when the annotation is removed on UPDATE operation.
// Uses connection type to determine exactly what needs to be cleaned up.
func (w *ISVCConnectionWebhook) performConnectionCleanup(
	ctx context.Context,
	req admission.Request,
	decodedObj *unstructured.Unstructured,
	connInfo webhookutils.ConnectionInfo,
) (bool, error) {
	log := logf.FromContext(ctx)
	log.V(1).Info("Performing connection cleanup for removed annotation", "name", req.Name, "namespace", req.Namespace, "oldConnectionType", connInfo.Type)

	cleanupPerformed := true

	// Clean up based on the old connection type or just try cleanup all.
	switch connInfo.Type {
	case "":
		// no old connection-type-ref means we do not know what was there so we need do a full cleanup
		// to ensure before injecting the new one
		// for oci:
		if connInfo.SecretName != "" {
			if err := w.Webhook.CleanupOCIImagePullSecrets(decodedObj, IsvcConfigs.ImagePullSecretPath, connInfo.SecretName); err != nil {
				return false, fmt.Errorf("failed to cleanup OCI .spec.predictor.imagePullSecrets: %w", err)
			}
			log.V(1).Info("Successfully cleaned up OCI .spec.predictor.imagePullSecrets", "secretName", connInfo.SecretName, "name", req.Name, "namespace", req.Namespace)
		} else {
			// If we don't know the secret name, remove the entire imagePullSecrets field as fallback
			unstructured.RemoveNestedField(decodedObj.Object, IsvcConfigs.ImagePullSecretPath...)
			log.V(1).Info("Successfully cleaned up entire .spec.predictor.imagePullSecrets field", "name", req.Name, "namespace", req.Namespace)
		}

		// for uri:
		if err := w.cleanupURIStorageUri(decodedObj); err != nil {
			return false, fmt.Errorf("failed to cleanup URI .spec.predictor.model.storageUri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up URI .spec.predictor.model.storageUri", "name", req.Name, "namespace", req.Namespace)

		// for s3: clean up ServiceAccountName injection
		if err := w.Webhook.HandleSA(decodedObj, IsvcConfigs.ServiceAccountNamePath, ""); err != nil {
			return false, fmt.Errorf("failed to cleanup .spec.predictor.serviceAccountName: %w", err)
		}
		if err := w.cleanupS3StorageKey(decodedObj); err != nil {
			return false, fmt.Errorf("failed to cleanup S3 .spec.predictor.model.storage: %w", err)
		}
		log.V(1).Info("Successfully cleaned up S3 .spec.predictor.model.storage", "name", req.Name, "namespace", req.Namespace)

	case webhookutils.ConnectionTypeProtocolOCI.String(), webhookutils.ConnectionTypeRefOCI.String():
		if err := w.Webhook.CleanupOCIImagePullSecrets(decodedObj, IsvcConfigs.ImagePullSecretPath, connInfo.SecretName); err != nil {
			return false, fmt.Errorf("failed to cleanup OCI .spec.predictor.imagePullSecrets: %w", err)
		}
		log.V(1).Info("Successfully cleaned up OCI .spec.predictor.imagePullSecrets", "secretName", connInfo.SecretName, "name", req.Name, "namespace", req.Namespace)

	case webhookutils.ConnectionTypeProtocolURI.String(), webhookutils.ConnectionTypeRefURI.String():
		if err := w.cleanupURIStorageUri(decodedObj); err != nil {
			return false, fmt.Errorf("failed to cleanup URI .spec.predictor.model.storageUri: %w", err)
		}
		log.V(1).Info("Successfully cleaned up URI .spec.predictor.model.storageUri", "name", req.Name, "namespace", req.Namespace)

	case webhookutils.ConnectionTypeProtocolS3.String(), webhookutils.ConnectionTypeRefS3.String():
		// remove ServiceAccountName injection, if we need it in replacement, we ill add it back later.
		if err := w.Webhook.HandleSA(decodedObj, IsvcConfigs.ServiceAccountNamePath, ""); err != nil {
			return false, fmt.Errorf("failed to cleanup .spec.predictor.serviceAccountName: %w", err)
		}
		// for isvc, we do not handle connection-path, as no cleanup or injectoin for .spec.predictor.model.path
		if err := w.cleanupS3StorageKey(decodedObj); err != nil {
			return false, fmt.Errorf("failed to cleanup S3 .spec.predictor.model.storage: %w", err)
		}
		log.V(1).Info("Successfully cleaned up S3 .spec.predictor.model.storage", "name", req.Name, "namespace", req.Namespace)

	default:
		// No specific cleanup needed for unknown connection types
		log.V(1).Info("No specific cleanup needed for connection type", "connectionType", connInfo.Type)
		cleanupPerformed = false
	}

	return cleanupPerformed, nil
}

// cleanupURIStorageUri delete the storageUri field from spec.predictor.model.
// cannot just set to empty string, it will fail in ValidateStorageURI().
func (w *ISVCConnectionWebhook) cleanupURIStorageUri(obj *unstructured.Unstructured) error {
	model, found, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return fmt.Errorf("failed to get .spec.predictor.model: %w", err)
	}
	if !found {
		return nil
	}

	// Remove the storageUri field
	delete(model, "storageUri")

	return webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath)
}

// cleanupS3StorageKey removes the storage field from spec.predictor.model.
func (w *ISVCConnectionWebhook) cleanupS3StorageKey(obj *unstructured.Unstructured) error {
	model, found, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return fmt.Errorf("failed to get .spec.predictor.model: %w", err)
	}
	if !found {
		return nil
	}

	// Remove the storage field
	delete(model, "storage")

	return webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath)
}

// injectURIStorageUri injects storageUri into spec.predictor.model.storageUri for URI connections.
func (w *ISVCConnectionWebhook) injectURIStorageUri(ctx context.Context, obj *unstructured.Unstructured, secretName, namespace string) error {
	uriValue, err := w.Webhook.GetURIValue(ctx, obj, secretName, namespace)
	if err != nil {
		return fmt.Errorf("failed to get URI value: %w", err)
	}
	if uriValue == "" {
		return fmt.Errorf("no connection type URI value found for secret %s", secretName)
	}

	// The secret data is already base64 decoded by Kubernetes, so we can use it directly.
	if err := webhookutils.SetNestedValue(obj.Object, uriValue, IsvcConfigs.StorageUriPath); err != nil {
		return fmt.Errorf("failed to set .spec.predictor.model.storageUri: %w", err)
	}
	return nil
}

// injectS3StorageKey injects storage key into spec.predictor.model.storage.key for S3 connections.
func (w *ISVCConnectionWebhook) injectS3StorageKeyPath(obj *unstructured.Unstructured, connInfo webhookutils.ConnectionInfo, oldSpecPath string) error {
	model, found, err := unstructured.NestedMap(obj.Object, IsvcConfigs.ModelPath...)
	if err != nil {
		return fmt.Errorf("failed to get .spec.predictor.model: %w", err)
	}
	if !found {
		return errors.New("not found .spec.predictor.model in resource")
	}

	storageMap, err := webhookutils.GetOrCreateNestedMap(model, "storage")
	if err != nil {
		return fmt.Errorf("failed to get or create nested map for storage: %w", err)
	}
	storageMap["key"] = connInfo.SecretName

	// injection priority order:
	// 1. if user is going toset .spec.predictor.model.storage.path, use it
	// 2. if user is going toadd annotation connection-path, use it
	// 3. if old isvc has .spec.predictor.model.storage.path, use it
	// 4. if none of the above, do nothing.
	currentPath, hasCurrentPath := storageMap["path"]
	switch {
	case hasCurrentPath && currentPath != "":
	case connInfo.Path != "":
		storageMap["path"] = connInfo.Path
	case oldSpecPath != "":
		storageMap["path"] = oldSpecPath
	}
	// If none of the above conditions are met, no path is set
	model["storage"] = storageMap

	if err := webhookutils.SetNestedValue(obj.Object, model, IsvcConfigs.ModelPath); err != nil {
		return fmt.Errorf("failed to set .spec.predictor.model storage: %w", err)
	}
	return nil
}
