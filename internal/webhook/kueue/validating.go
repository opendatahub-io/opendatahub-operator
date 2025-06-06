//go:build !nowebhook

package kueue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
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
// - batch/v1: jobs
// - kubeflow.org/v1: mpijobs, mxjobs, paddlejobs, pytorchjobs, tfjobs, xgboostjobs, notebooks
// - ray.io/v1alpha1: rayjobs, rayclusters
// - jobset.x-k8s.io/v1alpha2: jobsets
// - serving.kserve.io/v1beta1: inferenceservices

//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=batch,resources=jobs,verbs=create;update,versions=v1,name=job.kueuelabels.validator.opendatahub.io,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=kubeflow.org,resources=mpijobs;mxjobs;paddlejobs;pytorchjobs;tfjobs;xgboostjobs;notebooks,verbs=create;update,versions=v1,name=kubeflow.kueuelabels.validator.opendatahub.io,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=ray.io,resources=rayjobs;rayclusters,verbs=create;update,versions=v1alpha1,name=ray.kueuelabels.validator.opendatahub.io,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha2,name=jobset.kueuelabels.validator.opendatahub.io,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/validate-kueue,mutating=false,failurePolicy=fail,sideEffects=None,groups=serving.kserve.io,resources=inferenceservices,verbs=create;update,versions=v1beta1,name=serving.kserve.kueuelabels.validator.opendatahub.io,admissionReviewVersions=v1
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

// isKueueInstalled checks if Kueue is installed by looking for DataScienceCluster resources.
//
// Parameters:
//   - ctx: Context for the API call
//   - cli: The controller-runtime client to use for checking Kueue installation status
//
// Returns:
//   - bool: true if Kueue is installed, false otherwise
//   - error: Any error encountered while checking Kueue installation status
func isKueueInstalled(ctx context.Context, cli client.Reader) (bool, error) {
	dsc, err := cluster.GetDSC(ctx, cli)
	if err != nil {
		return false, err
	}

	state := dsc.Status.Components.Kueue.ManagementState
	return state == operatorv1.Managed || state == operatorv1.Unmanaged, nil
}

// isKueueEnabled checks if the given namespace is labeled for Kueue management.
//
// Parameters:
//   - ns: The namespace metadata to check for Kueue labels
//
// Returns:
//   - bool: true if the namespace is labeled for Kueue management, false otherwise
func isKueueEnabled(ns client.Object) bool {
	return resources.HasLabel(ns, KueueManagedLabelKey, "true") ||
		resources.HasLabel(ns, KueueLegacyManagedLabelKey, "true")
}

// isNamespaceKueueEnabled checks if the given namespace is labeled for Kueue management.
//
// Parameters:
//   - ctx: Context for the API call
//   - cli: The controller-runtime client to use for checking the namespace labels
//   - namespace: The name of the namespace to check
//
// Returns:
//   - bool: true if the namespace is labeled for Kueue, false otherwise
//   - error: Any error encountered while checking the namespace labels
func isNamespaceKueueEnabled(ctx context.Context, cli client.Reader, namespace string) (bool, error) {
	ns := &metav1.PartialObjectMetadata{}
	ns.SetGroupVersionKind(gvk.Namespace)

	if err := cli.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		// Unable to get the namespace, return an error
		return false, err
	}

	return isKueueEnabled(ns), nil
}

// validateLabels checks if the required Kueue labels are present and valid.
//
// Parameters:
//   - labels: The map of labels to validate
//
// Returns:
//   - error: If the required label is missing or empty, returns an error
func validateLabels(labels map[string]string) error {
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
//   - cli: The controller-runtime client to use for checking Kueue installation and namespace labels
//   - req: The admission.Request containing the operation and object details
//
// Returns:
//   - admission.Response: The result of the label validation, indicating whether the operation is allowed or denied
func performLabelValidation(ctx context.Context, cli client.Reader, req *admission.Request) admission.Response {
	namespace := req.Namespace

	// Check if Kueue is installed
	kueueInstalled, err := isKueueInstalled(ctx, cli)
	if err != nil {
		// Unable to determine if Kueue is installed, return an error response
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to check if Kueue is installed: %w", err))
	}

	// Check if the namespace is labeled for Kueue management
	kueueEnabled, err := isNamespaceKueueEnabled(ctx, cli, namespace)
	if err != nil {
		// Unable to determine if the namespace is labeled for Kueue, return an error response
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to check if namespace %q is labeled for Kueue: %w", namespace, err))
	}

	// Decode the object metadata from the request
	meta, err := decodeObjectMeta(req)
	if err != nil {
		// Unable to decode the object metadata, return an error response
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode object metadata: %w", err))
	}

	// Check if the object is a Kueue workload-related resource
	labelErr := validateLabels(meta.GetLabels())
	hasKueueLabels := labelErr == nil

	switch {
	case kueueInstalled && kueueEnabled && !hasKueueLabels:
		// Kueue labels found, namespace is labeled for Kueue, but label validation failed
		return admission.Denied(fmt.Sprintf("Kueue label validation failed: %v", labelErr))
	case hasKueueLabels && !kueueInstalled:
		// Kueue labels found but Kueue is not installed
		return admission.Denied(fmt.Sprintf("Kueue is not installed but workload %q is using Kueue label", req.Kind.Kind))
	case hasKueueLabels && !kueueEnabled:
		// Kueue labels found but namespace is not labeled for Kueue
		return admission.Denied(fmt.Sprintf("Namespace %q is not labeled for Kueue (%q) but workload %q is using Kueue label", namespace, KueueManagedLabelKey, req.Kind.Kind))
	case kueueInstalled && kueueEnabled && hasKueueLabels:
		// Kueue labels found, namespace is labeled for Kueue, and label validation passed
		return admission.Allowed(fmt.Sprintf("Kueue label validation passed for %q in namespace %q", req.Kind.Kind, namespace))
	default:
		// Kueue not installed or namespace not enabled, and no Kueue label — allow
		return admission.Allowed("Kueue label validation skipped (no Kueue installation or namespace labeling, and no workload label present)")
	}
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
