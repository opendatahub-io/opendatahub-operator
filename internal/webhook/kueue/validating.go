//go:build !nowebhook

package kueue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/shared"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// Webhooks for Kueue label validation:
// - kubeflow.org/v1: pytorchjobs, notebooks
// - ray.io/v1alpha1: rayjobs, rayclusters
// - serving.kserve.io/v1beta1: inferenceservices

//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=kubeflow.org,resources=pytorchjobs;notebooks,verbs=create;update,versions=v1,name=kubeflow-kueuelabels-validator.opendatahub.io,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayjobs;rayclusters,verbs=create;update,versions=v1alpha1,name=ray-kueuelabels-validator.opendatahub.io,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=serving.kserve.io,resources=inferenceservices,verbs=create;update,versions=v1beta1,name=kserve-kueuelabels-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

const (
	// Kueue component name used in DataScienceCluster status.
	KueueComponentName = componentApi.KueueComponentName

	// KueueManagedLabelKey indicates a namespace is managed by Kueue.
	KueueManagedLabelKey = "kueue.openshift.io/managed"

	// KueueLegacyManagedLabelKey is the legacy label key used to indicate a namespace is managed by Kueue.
	KueueLegacyManagedLabelKey = "kueue-managed"

	// KueueQueueNameLabelKey is the label key used to specify the Kueue queue name for workloads.
	KueueQueueNameLabelKey = "kueue.x-k8s.io/queue-name"
)

var (
	// Error messages for Kueue label validation.
	ErrMissingRequiredLabel = fmt.Errorf("missing required label %q", KueueQueueNameLabelKey)
	ErrEmptyRequiredLabel   = fmt.Errorf("label %q is set but empty", KueueQueueNameLabelKey)
)

// decodeObjectMeta decodes the object metadata from the admission request.
//
// Parameters:
//   - req: The admission.Request containing the object details
//
// Returns:
//   - *metav1.PartialObjectMetadata: The decoded object metadata
//   - error: Any error encountered while decoding the object metadata
func decodeObjectMeta(req *admission.Request) (*metav1.PartialObjectMetadata, error) {
	var meta metav1.PartialObjectMetadata
	if err := json.Unmarshal(req.Object.Raw, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
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

// validateNamespaceLabels checks if the given namespace is labeled for Kueue management.
//
// Parameters:
//   - ns: The namespace metadata to check for Kueue labels
//
// Returns:
//   - bool: true if the namespace is labeled for Kueue management, false otherwise
func validateNamespaceLabels(ns client.Object) bool {
	return resources.HasLabel(ns, KueueManagedLabelKey, "true") ||
		resources.HasLabel(ns, KueueLegacyManagedLabelKey, "true")
}

// isNamespaceManagedByKueue checks if the given namespace is labeled for Kueue management.
//
// Parameters:
//   - ctx: Context for the API call
//   - cli: The controller-runtime client to use for checking the namespace labels
//   - namespace: The name of the namespace to check
//
// Returns:
//   - bool: true if the namespace is labeled for Kueue, false otherwise
//   - error: Any error encountered while checking the namespace labels
func isNamespaceManagedByKueue(ctx context.Context, cli client.Reader, namespace string) (bool, error) {
	ns := &metav1.PartialObjectMetadata{}
	ns.SetGroupVersionKind(gvk.Namespace)

	if err := cli.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		// Unable to get the namespace, return an error
		return false, err
	}

	return validateNamespaceLabels(ns), nil
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
//   - cli: The controller-runtime client to use for checking Kueue status and namespace labels
//   - req: The admission.Request containing the operation and object details
//
// Returns:
//   - admission.Response: The result of the label validation, indicating whether the operation is allowed or denied
func performLabelValidation(ctx context.Context, cli client.Reader, req *admission.Request) admission.Response {
	namespace := req.Namespace

	// Decode the object metadata from the request
	meta, err := decodeObjectMeta(req)
	if err != nil {
		// Unable to decode the object metadata, return an error response
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode object metadata: %w", err))
	}

	// Check if the namespace is labeled for Kueue management
	// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-27558
	kueueManagedNamespace, err := isNamespaceManagedByKueue(ctx, cli, namespace)
	if err != nil {
		// Unable to determine if the namespace is labeled for Kueue, return an error response
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to check if namespace %q is labeled for Kueue: %w", namespace, err))
	}

	if !kueueManagedNamespace {
		// Namespace is not labeled for Kueue
		return admission.Allowed(fmt.Sprintf("Namespace %q is not labeled for Kueue (%q), skipping Kueue label validation", namespace, KueueManagedLabelKey))
	}

	// Check if Kueue is enabled in the DataScienceCluster (DSC)
	kueueEnabled, err := isKueueEnabledInDSC(ctx, cli)

	switch {
	case err != nil && k8serr.IsNotFound(err):
		// DSC not found — skip validation
		return admission.Allowed("No DataScienceCluster found, skipping Kueue label validation")
	case err != nil:
		// Unable to determine if Kueue is enabled, return an error response
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to check if Kueue is enabled: %w", err))
	case !kueueEnabled:
		// Kueue is not enabled in the DSC
		return admission.Allowed("Kueue is not enabled in DSC, skipping Kueue label validation")
	}

	// Check if the workload has Kueue labels
	if err := validateKueueLabels(meta.GetLabels()); err != nil {
		// No Kueue labels found
		return admission.Denied(fmt.Sprintf("Kueue label validation failed: %v", err))
	}

	// Kueue is enabled, namespace is labeled for Kueue, and workload has Kueue labels
	return admission.Allowed(fmt.Sprintf("Kueue label validation passed for %q in namespace %q", req.Kind.Kind, namespace))
}

// Validator implements webhook.AdmissionHandler for Kueue validation webhooks.
type Validator struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
}

// Assert that Validator implements admission.Handler interface.
var _ admission.Handler = &Validator{}

// InjectDecoder implements admission.DecoderInjector so the manager can inject the decoder automatically.
//
// Parameters:
//   - d: The admission.Decoder to inject.
//
// Returns:
//   - error: Always nil.
func (v *Validator) InjectDecoder(d admission.Decoder) error {
	v.Decoder = d
	return nil
}

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
	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		resp = performLabelValidation(ctx, v.Client, &req)
	default:
		resp.Allowed = true
	}

	return resp
}
