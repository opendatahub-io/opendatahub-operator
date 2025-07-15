//go:build !nowebhook

package kueue

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/shared"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// Webhooks for Kueue label validation:
// - kubeflow.org/v1: pytorchjobs, notebooks
// - ray.io/v1: rayjobs, rayclusters
// - serving.kserve.io/v1beta1: inferenceservices

const (
	// KueueQueueNameLabelKey is the label key used to specify the Kueue queue name for workloads.
	KueueQueueNameLabelKey = "kueue.x-k8s.io/queue-name"

	// ValidateKueuePath is the path for the Kueue validating webhook.
	ValidateKueuePath = "/validate-kueue"
)

var (
	// Error messages for Kueue label validation.
	ErrMissingRequiredLabel = fmt.Errorf("missing required label %q", KueueQueueNameLabelKey)
	ErrEmptyRequiredLabel   = fmt.Errorf("label %q is set but empty", KueueQueueNameLabelKey)
)

// Validator implements webhook.AdmissionHandler for Kueue validation webhooks.
type Validator struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
}

// Assert that Validator implements admission.Handler interface.
var _ admission.Handler = &Validator{}

// SetupWithManager registers the validating webhook with the provided controller-runtime manager.
//
// Parameters:
//   - mgr: The controller-runtime manager to register the webhook with.
//
// Returns:
//   - error: Always nil (for future extensibility).
func (v *Validator) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/validate-kueue", &webhook.Admission{
		Handler:        v,
		LogConstructor: shared.NewLogConstructor(v.Name),
	})
	return nil
}

// Handle processes admission requests for create and update operations on kueue workload-related resources.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - req: The admission.Request containing the operation and object details.
//
// Returns:
//   - admission.Response: The result of the admission check, indicating whether the operation is allowed or denied.
func (v *Validator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Check if decoder is properly injected
	if v.Decoder == nil {
		log.Error(nil, "Decoder is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
	}

	// Validate that we're processing an expected resource kind
	if !isExpectedKind(req.Kind) {
		err := fmt.Errorf("unexpected kind: %s", req.Kind.Kind)
		log.Error(err, "got wrong kind", "group", req.Kind.Group, "version", req.Kind.Version, "kind", req.Kind.Kind)
		return admission.Errored(http.StatusBadRequest, err)
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		resp = v.performLabelValidation(ctx, &req)
	default:
		resp = admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
	}

	return resp
}

// isExpectedKind checks if the given GroupVersionKind is one of the expected resource types
// that the Kueue webhook should handle.
//
// Parameters:
//   - kind: The GroupVersionKind from the admission request
//
// Returns:
//   - bool: true if the kind is expected, false otherwise
func isExpectedKind(kind metav1.GroupVersionKind) bool {
	// List of expected resource types that the Kueue webhook should handle
	expectedGVKs := []schema.GroupVersionKind{
		gvk.Notebook,          // kubeflow.org/v1/Notebook
		gvk.PyTorchJob,        // kubeflow.org/v1/PyTorchJob
		gvk.RayJob,            // ray.io/v1alpha1/RayJob
		gvk.RayCluster,        // ray.io/v1alpha1/RayCluster
		gvk.InferenceServices, // serving.kserve.io/v1beta1/InferenceService
	}

	requestGVK := schema.GroupVersionKind{
		Group:   kind.Group,
		Version: kind.Version,
		Kind:    kind.Kind,
	}

	for _, expectedGVK := range expectedGVKs {
		if requestGVK == expectedGVK {
			return true
		}
	}

	return false
}

// isKueueEnabledInDSC checks if Kueue is enabled in the DataScienceCluster (DSC).
//
// Parameters:
//   - ctx: Context for the API call
//   - cli: The controller-runtime client to use for checking Kueue status in the DSC
//
// Returns:
//   - bool: true if Kueue is enabled, false otherwise
//   - error: Any error encountered while checking Kueue status in the DSC
func isKueueEnabledInDSC(ctx context.Context, cli client.Reader) (bool, error) {
	dsc, err := cluster.GetDSC(ctx, cli)
	if err != nil {
		return false, err
	}

	state := dsc.Status.Components.Kueue.ManagementState
	// Kueue is considered enabled if it is either Managed or Unmanaged
	return state == operatorv1.Managed || state == operatorv1.Unmanaged, nil
}

// validateKueueLabels checks if the required Kueue labels are present and valid.
//
// Parameters:
//   - labels: The map of labels to validate
//
// Returns:
//   - error: If the required label is missing or empty, returns an error
func validateKueueLabels(labels map[string]string) error {
	if labels == nil {
		// Labels map is nil, which means no labels are set
		return ErrMissingRequiredLabel
	}

	queueName, ok := labels[KueueQueueNameLabelKey]

	if !ok {
		// Required label is missing
		return ErrMissingRequiredLabel
	}

	if queueName == "" {
		// Required label is present but empty
		return ErrEmptyRequiredLabel
	}

	// All required labels are present and valid
	return nil
}

// performLabelValidation checks if the Kueue labels are present and valid for the given request.
//
// Parameters:
//   - ctx: Context for the admission request
//   - req: The admission.Request containing the operation and object details
//
// Returns:
//   - admission.Response: The result of the label validation, indicating whether the operation is allowed or denied
func (v *Validator) performLabelValidation(ctx context.Context, req *admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Decode the object using the injected decoder
	// We use unstructured.Unstructured since we handle multiple resource types
	obj := &unstructured.Unstructured{}
	if err := v.Decoder.Decode(*req, obj); err != nil {
		log.Error(err, "failed to decode object")
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode object: %w", err))
	}

	// Check if Kueue is enabled in the DataScienceCluster (DSC)
	kueueEnabled, err := isKueueEnabledInDSC(ctx, v.Client)

	switch {
	case err != nil && k8serr.IsNotFound(err):
		// DSC not found — skip validation
		return admission.Allowed("No DataScienceCluster found, skipping Kueue label validation")
	case err != nil:
		// Unable to determine if Kueue is enabled, return an error response
		log.Error(err, "failed to check if Kueue is enabled in DSC")
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to check if Kueue is enabled: %w", err))
	case !kueueEnabled:
		// Kueue is not enabled in the DSC
		return admission.Allowed("Kueue is not enabled in DSC, skipping Kueue label validation")
	}

	// Check if the workload has Kueue labels
	if err := validateKueueLabels(obj.GetLabels()); err != nil {
		// No Kueue labels found
		return admission.Denied(fmt.Sprintf("Kueue label validation failed: %v", err))
	}

	// Kueue is enabled and workload has Kueue labels
	return admission.Allowed(fmt.Sprintf("Kueue label validation passed for %q in namespace %q", req.Kind.Kind, req.Namespace))
}
