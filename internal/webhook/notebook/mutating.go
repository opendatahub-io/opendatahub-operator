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

const (
	Create string = "create"
	Delete string = "delete"
)

//+kubebuilder:webhook:path=/platform-connection-notebook,mutating=true,failurePolicy=fail,groups=kubeflow.org,resources=notebooks,verbs=create;update,versions=v1,name=connection-notebook.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for Notebook connection validation webhooks.
type NotebookWebhook struct {
	Client    client.Client // used to create SubjectAccessReview
	APIReader client.Reader // used to read secrets in namespaces that are not cached
	Decoder   admission.Decoder
	Name      string
}

type NotebookSecretReference struct {
	Secret corev1.SecretReference
	Action string // create or delete, used to determine if the secret should be added or removed from the envFrom
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

	// Skip processing if object is marked for deletion
	if !notebook.GetDeletionTimestamp().IsZero() {
		return admission.Allowed("Object marked for deletion, skipping connection logic")
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		validationResp, shouldInject, notebookSecretRefs := w.validateNotebookConnectionAnnotation(ctx, notebook, &req)
		if !validationResp.Allowed {
			return validationResp
		}

		// Skip proceeding to injection if shouldInject is false or the secretRefs nil
		if !shouldInject || notebookSecretRefs == nil {
			return admission.Allowed(fmt.Sprintf("Connection annotation validation passed in namespace %s for %s, no injection needed", req.Namespace, req.Kind.Kind))
		}

		// Perform connection injection
		injectionPerformed, obj, err := w.performConnectionInjection(notebook, notebookSecretRefs)
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
) (admission.Response, bool, []NotebookSecretReference) {
	log := logf.FromContext(ctx)

	annotationValue := resources.GetAnnotation(nb, annotations.Connection)
	if req.Operation == admissionv1.Create && annotationValue == "" {
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
		return admission.Denied(fmt.Sprintf("some of the connection secret(s) do not exist or are outside the Notebook's namespace: %s",
			strings.Join(secretExistsErrors, ", "))), false, nil
	}

	if len(permissionsErrors) > 0 {
		return admission.Denied(fmt.Sprintf("user does not have permission to access the following connection secret(s): %s", strings.Join(permissionsErrors, ", "))), false, nil
	}

	notebookSecretRefs, err := w.getNotebookSecretRefs(ctx, req, connectionSecrets)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to get notebook secret references: %w", err)), false, nil
	}

	return admission.Allowed("Connection permissions validated successfully"), true, notebookSecretRefs
}

func (w *NotebookWebhook) getNotebookSecretRefs(ctx context.Context, req *admission.Request, secretRefs []corev1.SecretReference) ([]NotebookSecretReference, error) {
	var notebookSecretRefs []NotebookSecretReference

	// Only UPDATE operation has old object and potential connection secrets that may need to be removed
	if req.Operation == admissionv1.Update {
		oldSecretRefs, err := w.getOldConnectionSecrets(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to get old secret references: %w", err)
		}

		secretActions := determineSecretActions(oldSecretRefs, secretRefs)
		// Append old secret refs to the new secret refs to handle the case where a secret is removed
		secretRefs := append(secretRefs, oldSecretRefs...)
		for _, secretRef := range secretRefs {
			notebookSecretRefs = append(notebookSecretRefs, NotebookSecretReference{
				Secret: secretRef,
				Action: secretActions[secretRefKey(secretRef)],
			})
		}
	} else {
		for _, secretRef := range secretRefs {
			notebookSecretRefs = append(notebookSecretRefs, NotebookSecretReference{
				Secret: secretRef,
				Action: Create,
			})
		}
	}
	return notebookSecretRefs, nil
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
	var secretExistsErrors []string
	var permissionErrors []string

	// Check if the secret exists
	secretExistsErrors, err := w.checkSecretExists(ctx, req, secretRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check secret exists: %w", err)
	}

	permissionErrors, err = w.checkUserHasPermission(ctx, req, secretRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check user has permission: %w", err)
	}

	return secretExistsErrors, permissionErrors, nil
}

func (w *NotebookWebhook) checkSecretExists(ctx context.Context, req *admission.Request, secretRefs []corev1.SecretReference) ([]string, error) {
	log := logf.FromContext(ctx)
	var secretExistsErrors []string

	for _, secretRef := range secretRefs {
		// First check if the secret is in the same namespace as the notebook
		// TODO: this can be removed once we support cross-namespace secret references.
		log.V(1).Info("checking that secret in the same namespace as the notebook CR")
		if secretRef.Namespace != req.Namespace {
			secretExistsErrors = append(secretExistsErrors, fmt.Sprintf("%s/%s", secretRef.Namespace, secretRef.Name))
			continue
		}
		// Second check if the secret even exists using APIReader to bypass cache
		log.V(1).Info("checking that secret exists", "secret", secretRef.Name, "namespace", secretRef.Namespace)
		if err := w.APIReader.Get(ctx, client.ObjectKey{Namespace: secretRef.Namespace, Name: secretRef.Name}, &corev1.Secret{}); err != nil {
			if k8serr.IsNotFound(err) {
				secretExistsErrors = append(secretExistsErrors, fmt.Sprintf("%s/%s", secretRef.Namespace, secretRef.Name))
				continue
			}
			log.Error(err, "failed to check if secret exists", "secret", secretRef.Name, "namespace", secretRef.Namespace)
			return nil, fmt.Errorf("failed to check if secret exists: %w", err)
		}
	}

	return secretExistsErrors, nil
}

func (w *NotebookWebhook) checkUserHasPermission(ctx context.Context, req *admission.Request, secretRefs []corev1.SecretReference) ([]string, error) {
	log := logf.FromContext(ctx)

	var permissionErrors []string

	for _, secretRef := range secretRefs {
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
			return nil, fmt.Errorf("failed to create SubjectAccessReview: %w", err)
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

	return permissionErrors, nil
}

func (w *NotebookWebhook) performConnectionInjection(nb *unstructured.Unstructured, notebookSecretRefs []NotebookSecretReference) (bool, *unstructured.Unstructured, error) {
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

	existingEnvFrom, _ := container["envFrom"].([]interface{})
	for _, nbSecretRef := range notebookSecretRefs {
		existingEnvFrom = handleConnectionSecret(nbSecretRef, existingEnvFrom)
	}
	container["envFrom"] = existingEnvFrom

	// Update the first container in the containers array
	containers[0] = container

	// Set the modified containers array back to the object
	if err := unstructured.SetNestedSlice(nb.Object, containers, NotebookContainersPath...); err != nil {
		return false, nil, fmt.Errorf("failed to set containers array: %w", err)
	}

	return true, nb, nil
}

// determineSecretActions compares old and current secret references to determine
// which secrets need to be created, updated, or deleted.
func determineSecretActions(oldSecretRefs, currentSecretRefs []corev1.SecretReference) map[string]string {
	actions := make(map[string]string)

	// Create maps for easier lookup
	oldSecretsMap := make(map[string]corev1.SecretReference)
	currentSecretsMap := make(map[string]corev1.SecretReference)

	// Populate old secrets map
	for _, secretRef := range oldSecretRefs {
		key := secretRefKey(secretRef)
		oldSecretsMap[key] = secretRef
	}

	// Populate current secrets map and determine actions
	for _, secretRef := range currentSecretRefs {
		key := secretRefKey(secretRef)
		currentSecretsMap[key] = secretRef

		if _, existsInOld := oldSecretsMap[key]; !existsInOld {
			// Secret is new - mark for creation
			actions[key] = Create
		}
	}

	// Check for secrets that were removed (exist in old but not in current)
	for _, oldSecretRef := range oldSecretRefs {
		key := secretRefKey(oldSecretRef)
		if _, existsInCurrent := currentSecretsMap[key]; !existsInCurrent {
			// Secret was removed - mark for deletion
			actions[key] = Delete
		}
	}

	return actions
}

// secretRefKey creates a unique key for a secret reference.
func secretRefKey(secretRef corev1.SecretReference) string {
	return fmt.Sprintf("%s/%s", secretRef.Namespace, secretRef.Name)
}

// handleConnectionSecret adds or removes a connection secret from the envFrom based on the secret action.
func handleConnectionSecret(nbSecretRef NotebookSecretReference, existingEnvFrom []interface{}) []interface{} {
	switch nbSecretRef.Action {
	case Create:
		secretEntry := map[string]interface{}{
			"secretRef": map[string]interface{}{
				"name": nbSecretRef.Secret.Name,
			},
		}
		existingEnvFrom = append(existingEnvFrom, secretEntry)
	case Delete:
		for i, entry := range existingEnvFrom {
			if entryMap, ok := entry.(map[string]interface{}); ok {
				if secretRef, hasSecret := entryMap["secretRef"]; hasSecret {
					if secretRefMap, ok := secretRef.(map[string]interface{}); ok {
						if name, hasName := secretRefMap["name"]; hasName && name == nbSecretRef.Secret.Name {
							existingEnvFrom = append(existingEnvFrom[:i], existingEnvFrom[i+1:]...)
							break
						}
					}
				}
			}
		}
	}
	return existingEnvFrom
}

// getOldSecretRefs extracts connection secret references from the old notebook object
// for UPDATE operations to determine which secrets are being added/removed.
func (w *NotebookWebhook) getOldConnectionSecrets(ctx context.Context, req *admission.Request) ([]corev1.SecretReference, error) {
	log := logf.FromContext(ctx)
	// Decode the old notebook object
	oldNotebook := &unstructured.Unstructured{}
	if err := w.Decoder.DecodeRaw(req.OldObject, oldNotebook); err != nil {
		log.Error(err, "failed to decode old notebook object")
		return nil, fmt.Errorf("failed to decode old notebook object: %w", err)
	}

	oldAnnotationValue := resources.GetAnnotation(oldNotebook, annotations.Connection)
	if oldAnnotationValue == "" {
		return []corev1.SecretReference{}, nil
	}

	oldSecretRefs, err := parseConnectionsAnnotation(oldAnnotationValue)
	if err != nil {
		log.Error(err, "failed to parse old secret references", "annotationValue", oldAnnotationValue)
		return nil, fmt.Errorf("failed to parse old secret references: %w", err)
	}

	log.V(1).Info("Successfully parsed old secret references", "count", len(oldSecretRefs), "secretRefs", oldSecretRefs)
	return oldSecretRefs, nil
}
