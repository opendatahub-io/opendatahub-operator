//go:build !nowebhook

package notebook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

var (
	NotebookContainersPath = []string{"spec", "template", "spec", "containers"}
)

//+kubebuilder:webhook:path=/platform-connection-notebook,mutating=true,failurePolicy=fail,groups=kubeflow.org,resources=notebooks,verbs=create;update,versions=v1,name=connection-notebook.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for Notebook connection validation webhooks.
type NotebookWebhook struct {
	Client  client.Client
	Decoder admission.Decoder
	Name    string
}

// Assert that NotebookWebhook implements admission.Handler interface.
var _ admission.Handler = &NotebookWebhook{}

func (w *NotebookWebhook) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/platform-connection-notebook", &webhook.Admission{
		Handler:        w,
		LogConstructor: webhookutils.NewWebhookLogConstructor(w.Name),
	})
	return nil
}

func (w *NotebookWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	if w.Decoder == nil {
		log.Error(nil, "Decoder is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
	}

	// Validate that we're processing the correct Kind
	if req.Kind.Kind != gvk.Notebook.Kind {
		err := fmt.Errorf("unexpected kind: %s", req.Kind.Kind)
		log.Error(err, "got wrong kind", "group", req.Kind.Group, "version", req.Kind.Version, "kind", req.Kind.Kind)
		return admission.Errored(http.StatusBadRequest, err)
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		validationResp, shouldInject, secretRefs := w.validateNotebookConnectionAnnotation(ctx, &req)
		if !validationResp.Allowed {
			return validationResp
		}

		// Only proceed with injection if the annotation is valid and shouldInject is true
		if !shouldInject {
			return admission.Allowed(fmt.Sprintf("Connection annotation validation passed in namespace %s for %s, no injection needed", req.Namespace, req.Kind.Kind))
		}

		injectionPerformed, obj, err := w.performConnectionInjection(req, secretRefs)
		if err != nil {
			log.Error(err, "Failed to perform connection injection")
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
		resp = admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
	}

	return resp
}

// validateNotebookConnectionAnnotation validates the connection annotation "opendatahub.io/connections"
// If the annotation exists and has a non-empty value, it validates that the value references valid secret(s).
// Additionally, it checks that user requesting the notebook operation has the required permissions to get the secret(s)
// If the annotation doesn't exist or is empty, it allows the operation.
func (w *NotebookWebhook) validateNotebookConnectionAnnotation(ctx context.Context, req *admission.Request) (admission.Response, bool, []corev1.SecretReference) {
	log := logf.FromContext(ctx)

	// Decode the object from the request
	obj := &unstructured.Unstructured{}
	if err := w.Decoder.Decode(*req, obj); err != nil {
		log.Error(err, "failed to decode object")
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode object: %w", err)), false, nil
	}

	// Get the connections annotation
	objAnnotations := obj.GetAnnotations()
	if objAnnotations == nil {
		// No annotations - allow the request
		return admission.Allowed("No annotations found - connections validation skipped"), false, nil
	}

	connectionsValue, found := objAnnotations[annotations.Connection]
	if !found {
		// No connections annotation - allow the request
		return admission.Allowed("No connections annotation found - connections validation skipped"), false, nil
	}

	if strings.TrimSpace(connectionsValue) == "" {
		// Empty connections annotation - allow the request
		return admission.Allowed("Empty connections annotation - connections validation skipped"), false, nil
	}

	// Parse the connections annotation
	connectionSecrets, err := parseConnectionsAnnotation(connectionsValue)
	if err != nil {
		log.Error(err, "failed to parse connections annotation", "connectionsValue", connectionsValue)
		return admission.Denied(fmt.Sprintf("failed to parse connections annotation: %v", err)), false, nil
	}

	// Validate permissions for each connection secret
	var permissionErrors []string
	secretRefs := make([]corev1.SecretReference, 0)
	for _, secretRef := range connectionSecrets {
		log.V(1).Info("checking permission for secret", "secret", secretRef.Name, "namespace", secretRef.Namespace)
		hasPermission, err := w.checkSecretPermission(ctx, req, secretRef)
		if err != nil {
			log.Error(err, "error checking permission for secret", "secret", secretRef.Name, "namespace", secretRef.Namespace)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error checking permission for secret %s/%s: %w", secretRef.Namespace, secretRef.Name, err)), false, nil
		}

		if !hasPermission {
			permissionErrors = append(permissionErrors, fmt.Sprintf("%s/%s", secretRef.Namespace, secretRef.Name))
		}

		secretRefs = append(secretRefs, secretRef)
	}

	if len(permissionErrors) > 0 {
		return admission.Denied(fmt.Sprintf("user does not have permission to access the following connection secrets: %s", strings.Join(permissionErrors, ", "))), false, nil
	}

	return admission.Allowed("Connection permissions validated successfully"), true, secretRefs
}

// parseConnectionsAnnotation parses the connections annotation value into a list of secret references.
// The annotation value should be a comma-separated list of fully qualified secret names (namespace/name).
func parseConnectionsAnnotation(value string) ([]corev1.SecretReference, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	secretRefs := make([]corev1.SecretReference, 0)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Parse namespace/name format
		secretParts := strings.Split(part, "/")
		if len(secretParts) != 2 {
			return nil, fmt.Errorf("invalid secret reference format '%s' - expected 'namespace/name'", part)
		}

		namespace := strings.TrimSpace(secretParts[0])
		name := strings.TrimSpace(secretParts[1])

		if namespace == "" || name == "" {
			return nil, fmt.Errorf("invalid secret reference '%s' - namespace and name cannot be empty", part)
		}

		secretRefs = append(secretRefs, corev1.SecretReference{
			Name:      name,
			Namespace: namespace,
		})
	}

	return secretRefs, nil
}

// checkSecretPermission checks if the user has permission to "get" the specified secret using SubjectAccessReview.
func (w *NotebookWebhook) checkSecretPermission(ctx context.Context, req *admission.Request, secretRef corev1.SecretReference) (bool, error) {
	log := logf.FromContext(ctx)

	// Create a SubjectAccessReview to check if the user can "get" the secret
	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   req.UserInfo.Username,
			Groups: req.UserInfo.Groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: secretRef.Namespace,
				Verb:      "get",
				Group:     "",
				Version:   "v1",
				Resource:  "secrets",
				Name:      secretRef.Name,
			},
		},
	}

	// Send the SubjectAccessReview to the API server
	if err := w.Client.Create(ctx, sar); err != nil {
		log.Error(err, "failed to create SubjectAccessReview", "secret", secretRef.Name, "namespace", secretRef.Namespace)
		return false, fmt.Errorf("failed to create SubjectAccessReview: %w", err)
	}

	// Check the result
	if !sar.Status.Allowed {
		log.V(1).Info("user does not have permission to access secret",
			"secret", secretRef.Name,
			"namespace", secretRef.Namespace,
			"reason", sar.Status.Reason,
			"evaluationError", sar.Status.EvaluationError,
		)

		return false, nil
	}

	log.V(1).Info("user has permission to access secret", "secret", secretRef.Name, "namespace", secretRef.Namespace)
	return true, nil
}

func (w *NotebookWebhook) performConnectionInjection(req admission.Request, secretRefs []corev1.SecretReference) (bool, *unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := w.Decoder.Decode(req, obj); err != nil {
		return false, nil, fmt.Errorf("failed to decode Notebook object: %w", err)
	}

	if len(secretRefs) == 0 {
		return false, obj, nil
	}

	// Get the notebook containers
	containers, found, err := unstructured.NestedSlice(obj.Object, NotebookContainersPath...)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get containers array: %w", err)
	}
	if !found || len(containers) == 0 {
		return false, nil, errors.New("no containers found in notebook")
	}

	// The notebook only has one container, so we can get the envFrom from the first container
	container, ok := containers[0].(map[string]interface{})
	if !ok {
		return false, nil, errors.New("first container is not a map[string]interface{}")
	}

	// Get existing envFrom from the container
	existingEnvFrom, _ := container["envFrom"].([]interface{})

	// Keep only non-secretRef entries (like configMapRef)
	var preservedEntries []interface{}
	for _, entry := range existingEnvFrom {
		if entryMap, ok := entry.(map[string]interface{}); ok {
			if _, hasSecretRef := entryMap["secretRef"]; !hasSecretRef {
				preservedEntries = append(preservedEntries, entry)
			}
		}
	}

	// Add all connection secrets
	newEnvFrom := preservedEntries
	for _, secretRef := range secretRefs {
		secretEntry := map[string]interface{}{
			"secretRef": map[string]interface{}{
				"name": secretRef.Name,
			},
		}
		newEnvFrom = append(newEnvFrom, secretEntry)
	}

	// Set the updated envFrom back to the container
	container["envFrom"] = newEnvFrom

	// Update the first container in the containers array
	containers[0] = container

	// Set the modified containers array back to the object
	if err := unstructured.SetNestedSlice(obj.Object, containers, NotebookContainersPath...); err != nil {
		return false, nil, fmt.Errorf("failed to set containers array: %w", err)
	}

	return true, obj, nil
}
