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
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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

	// Decode the notebook from the request
	notebook := &unstructured.Unstructured{}
	if err := w.Decoder.Decode(req, notebook); err != nil {
		log.Error(err, "failed to decode object")
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode object: %w", err))
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		validationResp, shouldInject, secretRefs := w.validateNotebookConnectionAnnotation(ctx, notebook, &req)
		if !validationResp.Allowed {
			return validationResp
		}

		// Skip proceeding to injection if shouldInject is false or the secretRefs nil
		if !shouldInject || secretRefs == nil {
			return admission.Allowed(fmt.Sprintf("Connection annotation validation passed in namespace %s for %s, no injection needed", req.Namespace, req.Kind.Kind))
		}

		injectionPerformed, obj, err := w.performConnectionInjection(notebook, secretRefs)
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
func (w *NotebookWebhook) validateNotebookConnectionAnnotation(
	ctx context.Context,
	nb *unstructured.Unstructured,
	req *admission.Request,
) (admission.Response, bool, []corev1.SecretReference) {
	log := logf.FromContext(ctx)

	annotationValue := resources.GetAnnotation(nb, annotations.Connection)
	if annotationValue == "" {
		return admission.Allowed(fmt.Sprintf("Annotation '%s' not present or empty value, skipping validation", annotations.Connection)), false, nil
	}

	// Parse the connections annotation
	connectionSecrets, err := parseConnectionsAnnotation(annotationValue)
	if err != nil {
		log.Error(err, "failed to parse connections annotation", "annotationValue", annotationValue)
		return admission.Denied(fmt.Sprintf("failed to parse connections annotation: %v", err)), false, nil
	}

	// Validate each connection secret exists and the user has permission to get each secret
	secretExistsErrors, permissionsErrors, err := w.checkSecretsExistsAndUserHasPermissions(ctx, req, connectionSecrets)
	if err != nil {
		log.Error(err, "error verifying secret(s) exist or confirming user has get permissions for the secret(s)", "connectionSecrets", connectionSecrets)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error verifying secret(s) exist/user has permissions for them %s: %w", connectionSecrets, err)), false, nil
	}

	if len(secretExistsErrors) > 0 {
		return admission.Denied(fmt.Sprintf("some of the connection secret(s) do not exist: %s", strings.Join(secretExistsErrors, ", "))), false, nil
	}

	if len(permissionsErrors) > 0 {
		return admission.Denied(fmt.Sprintf("user does not have permission to access the following connection secret(s): %s", strings.Join(permissionsErrors, ", "))), false, nil
	}

	return admission.Allowed("Connection permissions validated successfully"), true, connectionSecrets
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

// checkSecretsExistsAndUserHasPermissions checks that each connection secret exists
// It also verifies that the user has permission to "get" the specified secrets using SubjectAccessReviews.
func (w *NotebookWebhook) checkSecretsExistsAndUserHasPermissions(ctx context.Context, req *admission.Request, secretRefs []corev1.SecretReference) ([]string, []string, error) {
	log := logf.FromContext(ctx)

	var secretExistsErrors []string
	var permissionErrors []string

	for _, secretRef := range secretRefs {
		// First check if the secret even exists
		log.V(1).Info("checking that secret exists", "secret", secretRef.Name, "namespace", secretRef.Namespace)
		if err := w.Client.Get(ctx, client.ObjectKey{Namespace: secretRef.Namespace, Name: secretRef.Name}, &corev1.Secret{}); err != nil {
			if k8serr.IsNotFound(err) {
				secretExistsErrors = append(secretExistsErrors, fmt.Sprintf("%s/%s", secretRef.Namespace, secretRef.Name))
				continue
			}
			log.Error(err, "failed to check if secret exists", "secret", secretRef.Name, "namespace", secretRef.Namespace)
			return nil, nil, fmt.Errorf("failed to check if secret exists: %w", err)
		}

		// Create a SubjectAccessReview to check if the user can "get" the secret
		log.V(1).Info("checking permission for secret", "secret", secretRef.Name, "namespace", secretRef.Namespace)
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

		// Send the SubjectAccessReview to the API server to verify permission
		if err := w.Client.Create(ctx, sar); err != nil {
			log.Error(err, "failed to create SubjectAccessReview", "secret", secretRef.Name, "namespace", secretRef.Namespace)
			return nil, nil, fmt.Errorf("failed to create SubjectAccessReview: %w", err)
		}

		// Check the result
		if !sar.Status.Allowed {
			log.V(1).Info("user does not have permission to access secret",
				"secret", secretRef.Name,
				"namespace", secretRef.Namespace,
				"reason", sar.Status.Reason,
				"evaluationError", sar.Status.EvaluationError,
			)
			permissionErrors = append(permissionErrors, fmt.Sprintf("%s/%s", secretRef.Namespace, secretRef.Name))
		}

		log.V(1).Info("user has permission to access secret", "secret", secretRef.Name, "namespace", secretRef.Namespace)
	}

	return secretExistsErrors, permissionErrors, nil
}

func (w *NotebookWebhook) performConnectionInjection(nb *unstructured.Unstructured, secretRefs []corev1.SecretReference) (bool, *unstructured.Unstructured, error) {
	// Get the notebook containers
	containers, found, err := unstructured.NestedSlice(nb.Object, NotebookContainersPath...)
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
	if err := unstructured.SetNestedSlice(nb.Object, containers, NotebookContainersPath...); err != nil {
		return false, nil, fmt.Errorf("failed to set containers array: %w", err)
	}

	return true, nb, nil
}
