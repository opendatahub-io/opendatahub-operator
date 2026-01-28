//go:build !nowebhook

package hardwareprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

// Annotation constants.
const (
	HardwareProfileNameAnnotation      = "opendatahub.io/hardware-profile-name"
	HardwareProfileNamespaceAnnotation = "opendatahub.io/hardware-profile-namespace"
)

// WorkloadConfig defines path configuration for different workload types.
type WorkloadConfig struct {
	ContainersPath   []string // .spec.identifiers from HWProfile
	NodeSelectorPath []string // .spec.scheduling.node.nodeSelector from HWProfile
	TolerationsPath  []string // .spec.scheduling.node.tolerations from HWProfile
}

// WorkloadConfigs maps Kubernetes resource kinds to their configuration paths.
var WorkloadConfigs = map[string]WorkloadConfig{
	gvk.Notebook.Kind: {
		ContainersPath:   []string{"spec", "template", "spec", "containers"}, // slice []interface{}
		NodeSelectorPath: []string{"spec", "template", "spec", "nodeSelector"},
		TolerationsPath:  []string{"spec", "template", "spec", "tolerations"},
	},
	gvk.InferenceServices.Kind: {
		ContainersPath:   []string{"spec", "predictor", "model"}, // map map[string]interface{}
		NodeSelectorPath: []string{"spec", "predictor", "nodeSelector"},
		TolerationsPath:  []string{"spec", "predictor", "tolerations"},
	},
	gvk.LLMInferenceServiceV1Alpha1.Kind: {
		ContainersPath:   []string{"spec", "template", "containers"}, // slice []interface{}
		NodeSelectorPath: []string{"spec", "template", "nodeSelector"},
		TolerationsPath:  []string{"spec", "template", "tolerations"},
	},
}

//+kubebuilder:webhook:path=/mutate-hardware-profile,mutating=true,failurePolicy=fail,groups=kubeflow.org,resources=notebooks,verbs=create;update,versions=v1,name=hardwareprofile-notebook-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/mutate-hardware-profile,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=inferenceservices,verbs=create;update,versions=v1beta1,name=hardwareprofile-isvc-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/mutate-hardware-profile,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=llminferenceservices,verbs=create;update,versions=v1alpha1,name=hardwareprofile-llmisvc-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

// Injector implements a mutating admission webhook for hardware profile injection.
type Injector struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
}

// Assert that Injector implements admission.Handler interface.
var _ admission.Handler = &Injector{}

// SetupWithManager registers the mutating webhook with the provided controller-runtime manager.
//
// Parameters:
//   - mgr: The controller-runtime manager to register the webhook with.
//
// Returns:
//   - error: Always nil (for future extensibility).
func (i *Injector) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()

	// Register single webhook path for Notebooks, InferenceServices, and LLMInferenceServices
	hookServer.Register("/mutate-hardware-profile", &webhook.Admission{
		Handler:        i,
		LogConstructor: webhookutils.NewWebhookLogConstructor(i.Name),
	})

	return nil
}

// Handle processes admission requests for workload resources with hardware profile annotations.
// This is the main entry point for the webhook and orchestrates the entire hardware profile
// injection process.
//
// The method performs the following operations:
//  1. Validates that the decoder is properly initialized
//  2. Checks if the resource kind is supported by the webhook
//  3. Routes CREATE and UPDATE operations to the injection logic
//  4. Allows all other operations (DELETE, CONNECT, etc.) without modification
//
// Error Handling:
//   - Returns HTTP 500 if the decoder is not initialized
//   - Returns HTTP 400 for unsupported resource kinds
//   - Delegates error handling to injection logic for supported operations
//
// Parameters:
//   - ctx: Request context containing logger and other contextual information
//   - req: The admission.Request containing operation type and resource details
//
// Returns:
//   - admission.Response: The result of the admission check with any mutations applied
func (i *Injector) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Check if decoder is properly injected
	if i.Decoder == nil {
		log.Error(nil, "Decoder is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
	}

	// Validate that we're processing an expected resource kind
	if !isExpectedKind(req.Kind) {
		err := fmt.Errorf("unexpected kind: %s", req.Kind.Kind)
		log.Error(err, "got wrong kind")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Decode the object
	obj, err := webhookutils.DecodeUnstructured(i.Decoder, req)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Skip processing if object is marked for deletion
	if !obj.GetDeletionTimestamp().IsZero() {
		return admission.Allowed("Object marked for deletion, skipping hardware profile injection")
	}

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		return i.performHardwareProfileInjection(ctx, &req, obj)
	default:
		return admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
	}
}

// isExpectedKind checks if the given GroupVersionKind is supported by the webhook.
//
// Parameters:
//   - kind: The GroupVersionKind from the admission request to validate
//
// Returns:
//   - bool: true if the kind is supported by the webhook, false otherwise
func isExpectedKind(kind metav1.GroupVersionKind) bool {
	// expectedGVKs contains the list of resource types that the hardware profile webhook should handle.
	expectedGVKs := []schema.GroupVersionKind{
		gvk.Notebook,                    // kubeflow.org/v1/Notebook
		gvk.InferenceServices,           // serving.kserve.io/v1beta1/InferenceService
		gvk.LLMInferenceServiceV1Alpha1, // serving.kserve.io/v1alpha1/LLMInferenceService
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

// performHardwareProfileInjection handles the core logic for hardware profile injection.
// This method orchestrates the entire process of applying hardwareprofile specifications
// to workload resources.
//
// The injection process follows these steps:
//  1. Check for hardware profile annotations on the object
//  2. Determine the namespace for the hardware profile lookup
//  3. Fetch the HardwareProfile resource from the Kubernetes API
//  4. Validate that the hardware profile has meaningful configuration
//  5. Set the hardware profile namespace annotation if not present
//  6. Detect if the hardware profile changed (on UPDATE operations)
//  7. Apply hardware profile specifications to the workload
//  8. Return the modified object as a patch response
//
// Annotation Handling:
//   - opendatahub.io/hardware-profile-name: Required annotation specifying the profile name
//   - opendatahub.io/hardware-profile-namespace: Optional annotation for cross-namespace profiles
//
// Profile Change Detection:
//   - On UPDATE operations, compares old and new hardware profile annotations
//   - When profile changes, clears existing scheduling configuration before applying new settings
//   - When profile is unchanged, merges tolerations to preserve manually-added ones
//
// Error Conditions:
//   - Returns HTTP 400 for object decoding failures or missing profile namespace
//   - Returns HTTP 400 for non-existent hardware profiles
//   - Returns HTTP 500 for internal errors during profile application or object marshaling
//
// Parameters:
//   - ctx: Request context containing logger and other contextual information
//   - req: The admission.Request containing the workload object and operation details
//
// Returns:
//   - admission.Response: Success response with object patch or error response with details
func (i *Injector) performHardwareProfileInjection(ctx context.Context, req *admission.Request, obj *unstructured.Unstructured) admission.Response {
	log := logf.FromContext(ctx)

	// Check if the object has hardware profile annotations
	profileName := resources.GetAnnotation(obj, HardwareProfileNameAnnotation)
	if profileName == "" {
		// Check if HWP annotation was removed (old object had it, new doesn't)
		if req.Operation == admissionv1.Update {
			if resp := i.handleHWPRemoval(ctx, req, obj); resp != nil {
				return *resp
			}
		}
		return admission.Allowed("No hardware profile annotation found")
	}

	// Determine the namespace for the hardware profile
	profileNamespace := resources.GetAnnotation(obj, HardwareProfileNamespaceAnnotation)
	if profileNamespace == "" {
		profileNamespace = obj.GetNamespace()
	}
	if profileNamespace == "" {
		return admission.Errored(http.StatusBadRequest, errors.New("unable to determine hardware profile namespace"))
	}

	// Get hardwareprofile.infrastructure.opendatahub.io/v1alpha1 CR
	hwp, err := i.fetchHardwareProfile(ctx, profileNamespace, profileName)
	if err != nil {
		if k8serr.IsNotFound(err) {
			log.V(1).Info("Hardware profile not found", "profile", profileName, "namespace", profileNamespace, "request", req.Name)
			userErr := fmt.Errorf("hardware profile '%s' not found in namespace '%s'", profileName, profileNamespace)
			return admission.Errored(http.StatusBadRequest, userErr)
		} else {
			log.Error(err, "Failed to get hardware profile", "profile", profileName, "namespace", profileNamespace)
			userErr := fmt.Errorf("failed to get hardware profile '%s' from namespace '%s': %w", profileName, profileNamespace, err)
			return admission.Errored(http.StatusInternalServerError, userErr)
		}
	}

	// Only set the annotation if it wasn't already set
	if resources.GetAnnotation(obj, HardwareProfileNamespaceAnnotation) == "" {
		resources.SetAnnotation(obj, HardwareProfileNamespaceAnnotation, profileNamespace)
	}

	// Detect if the hardware profile changed (only on UPDATE operations)
	profileChanged := i.detectProfileChange(req, profileName, profileNamespace)
	if profileChanged {
		log.V(1).Info("hardware profile changed, will clear existing scheduling settings",
			"workload", obj.GetName(), "newProfile", profileName, "newNamespace", profileNamespace)
	}

	// Apply hardware profile specifications
	warnings, err := i.applyHardwareProfileToWorkload(ctx, obj, hwp, profileChanged)
	if err != nil {
		log.Error(err, "Failed to apply hardware profile", "profile", profileName)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Marshal the modified object
	marshaledObj, err := json.Marshal(obj)
	if err != nil {
		log.Error(err, "Failed to marshal modified object")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Return the admission response with the modified object and any warnings
	resp := admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
	if len(warnings) > 0 {
		resp.Warnings = warnings
		log.V(1).Info("admission response includes warnings", "warnings", warnings)
	}
	return resp
}

// detectProfileChange checks if the hardware profile annotation changed during an UPDATE operation.
// This is used to determine whether to clear existing scheduling configuration before applying
// new settings (on profile change) or merge tolerations (when profile is unchanged).
//
// The function returns false (no change / merge behavior) in these cases:
//   - CREATE operation: No old object to compare
//   - Initial HWP assignment: Old object had no HWP annotation (preserves existing tolerations)
//   - Same profile: Old and new annotations match
//
// The function returns true (clear and replace behavior) when:
//   - Profile name changed: User switched from one HWP to another
//   - Profile namespace changed: User switched to a different namespace's HWP
//
// Parameters:
//   - req: The admission.Request containing both old and new object states
//   - newProfileName: The hardware profile name from the new object
//   - newProfileNamespace: The hardware profile namespace from the new object
//
// Returns:
//   - bool: true if switching profiles (triggers clearing), false otherwise (triggers merging)
func (i *Injector) detectProfileChange(req *admission.Request, newProfileName, newProfileNamespace string) bool {
	// On CREATE, there's no old object to compare - no clearing needed for new resources
	if req.Operation == admissionv1.Create {
		return false
	}

	// On UPDATE, if we somehow don't have the old object, assume profile changed to be safe
	if req.OldObject.Raw == nil {
		return true
	}

	oldObj := &unstructured.Unstructured{}
	if err := json.Unmarshal(req.OldObject.Raw, oldObj); err != nil {
		// If we can't unmarshal, assume profile changed to be safe
		return true
	}

	oldProfileName := resources.GetAnnotation(oldObj, HardwareProfileNameAnnotation)

	// If old object had no HWP annotation, this is an initial assignment, not a profile change.
	// We should merge tolerations rather than clear, to preserve any existing manual tolerations.
	if oldProfileName == "" {
		return false
	}

	oldProfileNamespace := resources.GetAnnotation(oldObj, HardwareProfileNamespaceAnnotation)

	// If old object had no namespace annotation, it defaulted to object's namespace
	if oldProfileNamespace == "" {
		oldProfileNamespace = oldObj.GetNamespace()
	}

	// Profile changed if either name or namespace differs
	return oldProfileName != newProfileName || oldProfileNamespace != newProfileNamespace
}

// handleHWPRemoval handles the case where an HWP annotation is removed from a workload.
// It fetches the old HWP using the old object's annotations and removes only the
// tolerations and nodeSelector entries that match the HWP's settings.
//
// Returns:
//   - *admission.Response: Response with patches if HWP was removed and cleanup performed, nil otherwise
func (i *Injector) handleHWPRemoval(ctx context.Context, req *admission.Request, obj *unstructured.Unstructured) *admission.Response {
	log := logf.FromContext(ctx)

	if req.OldObject.Raw == nil {
		return nil
	}

	oldObj := &unstructured.Unstructured{}
	if err := json.Unmarshal(req.OldObject.Raw, oldObj); err != nil {
		return nil
	}

	// Check if old object had HWP annotation
	oldProfileName := resources.GetAnnotation(oldObj, HardwareProfileNameAnnotation)
	if oldProfileName == "" {
		return nil // Old object didn't have HWP, nothing to clean up
	}

	log.V(1).Info("HWP annotation removed, cleaning up HWP-applied settings",
		"workload", obj.GetName(), "oldProfile", oldProfileName)

	// Get old HWP namespace
	oldProfileNamespace := resources.GetAnnotation(oldObj, HardwareProfileNamespaceAnnotation)
	if oldProfileNamespace == "" {
		oldProfileNamespace = oldObj.GetNamespace()
	}

	// Fetch the old HWP to know what to remove
	oldHWP, err := i.fetchHardwareProfile(ctx, oldProfileNamespace, oldProfileName)
	if err != nil {
		// If HWP is not found or can't be fetched, we can't clean up
		// Log a warning but allow the request (don't block user from removing annotation)
		log.V(1).Info("Could not fetch old HWP for cleanup, HWP-applied settings may remain",
			"error", err, "oldProfile", oldProfileName, "oldNamespace", oldProfileNamespace)
		// Still remove the namespace annotation
		resources.RemoveAnnotation(obj, HardwareProfileNamespaceAnnotation)
		marshaledObj, marshalErr := json.Marshal(obj)
		if marshalErr != nil {
			return nil
		}
		resp := admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		return &resp
	}

	// Clean up HWP-applied settings
	if err := i.removeHWPSettings(obj, oldHWP); err != nil {
		log.Error(err, "Failed to remove HWP settings")
		resp := admission.Errored(http.StatusInternalServerError, err)
		return &resp
	}

	// Remove the HWP namespace annotation
	resources.RemoveAnnotation(obj, HardwareProfileNamespaceAnnotation)

	// Marshal and return the modified object
	marshaledObj, err := json.Marshal(obj)
	if err != nil {
		log.Error(err, "Failed to marshal modified object")
		resp := admission.Errored(http.StatusInternalServerError, err)
		return &resp
	}

	resp := admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
	return &resp
}

// removeHWPSettings removes tolerations, nodeSelector, and Kueue label entries that were applied by the HWP.
// It compares tolerations/nodeSelector on the workload with those defined in the HWP
// and removes only the matching ones, preserving manually-added settings.
func (i *Injector) removeHWPSettings(obj *unstructured.Unstructured, hwp *infrav1.HardwareProfile) error {
	config, err := GetWorkloadConfig(obj.GetKind())
	if err != nil {
		return err
	}

	// Remove HWP-applied tolerations
	if hwp.Spec.SchedulingSpec != nil && hwp.Spec.SchedulingSpec.Node != nil {
		if len(hwp.Spec.SchedulingSpec.Node.Tolerations) > 0 {
			if err := i.removeHWPTolerations(obj, config.TolerationsPath, hwp.Spec.SchedulingSpec.Node.Tolerations); err != nil {
				return fmt.Errorf("failed to remove HWP tolerations: %w", err)
			}
		}

		// Remove HWP-applied nodeSelector
		if len(hwp.Spec.SchedulingSpec.Node.NodeSelector) > 0 {
			if err := i.removeHWPNodeSelector(obj, config.NodeSelectorPath, hwp.Spec.SchedulingSpec.Node.NodeSelector); err != nil {
				return fmt.Errorf("failed to remove HWP nodeSelector: %w", err)
			}
		}
	}

	// Remove Kueue label if HWP had Kueue scheduling
	if hwp.Spec.SchedulingSpec != nil && hwp.Spec.SchedulingSpec.Kueue != nil && hwp.Spec.SchedulingSpec.Kueue.LocalQueueName != "" {
		resources.RemoveLabel(obj, cluster.KueueQueueNameLabel)
	}

	return nil
}

// removeHWPTolerations removes tolerations from the workload that match the HWP's tolerations.
func (i *Injector) removeHWPTolerations(obj *unstructured.Unstructured, tolerationsPath []string, hwpTolerations []corev1.Toleration) error {
	existingTolerations, found, err := unstructured.NestedSlice(obj.Object, tolerationsPath...)
	if err != nil {
		return err
	}
	if !found || len(existingTolerations) == 0 {
		return nil // No tolerations to remove
	}

	// Build set of HWP toleration keys for matching
	hwpTolKeys := make(map[string]bool)
	for _, tol := range hwpTolerations {
		hwpTolKeys[tolerationKeyFromCoreV1(tol)] = true
	}

	// Keep only tolerations that don't match HWP tolerations
	remaining := make([]interface{}, 0, len(existingTolerations))
	for _, existing := range existingTolerations {
		if existingMap, ok := existing.(map[string]interface{}); ok {
			if !hwpTolKeys[TolerationKey(existingMap)] {
				remaining = append(remaining, existing)
			}
		}
	}

	// Update or remove tolerations field
	if len(remaining) == 0 {
		unstructured.RemoveNestedField(obj.Object, tolerationsPath...)
	} else {
		if err := unstructured.SetNestedSlice(obj.Object, remaining, tolerationsPath...); err != nil {
			return err
		}
	}

	return nil
}

// tolerationKeyFromCoreV1 generates a unique key for a corev1.Toleration.
// The key includes key, operator, value, effect, and tolerationSeconds to ensure
// tolerations with the same key/operator/effect but different values are treated as distinct.
// This prevents accidental removal of user-specified tolerations during HWP cleanup.
func tolerationKeyFromCoreV1(tol corev1.Toleration) string {
	ts := ""
	if tol.TolerationSeconds != nil {
		ts = strconv.FormatInt(*tol.TolerationSeconds, 10)
	}
	return fmt.Sprintf("%s:%s:%s:%s:%s", tol.Key, string(tol.Operator), tol.Value, string(tol.Effect), ts)
}

// removeHWPNodeSelector removes nodeSelector entries from the workload that match the HWP's nodeSelector.
func (i *Injector) removeHWPNodeSelector(obj *unstructured.Unstructured, nodeSelectorPath []string, hwpNodeSelector map[string]string) error {
	existingNodeSelector, found, err := unstructured.NestedStringMap(obj.Object, nodeSelectorPath...)
	if err != nil {
		return err
	}
	if !found || len(existingNodeSelector) == 0 {
		return nil // No nodeSelector to remove
	}

	// Remove keys that match HWP nodeSelector
	for key, value := range hwpNodeSelector {
		if existingValue, exists := existingNodeSelector[key]; exists && existingValue == value {
			delete(existingNodeSelector, key)
		}
	}

	// Update or remove nodeSelector field
	if len(existingNodeSelector) == 0 {
		unstructured.RemoveNestedField(obj.Object, nodeSelectorPath...)
	} else {
		if err := unstructured.SetNestedStringMap(obj.Object, existingNodeSelector, nodeSelectorPath...); err != nil {
			return err
		}
	}

	return nil
}

// fetchHardwareProfile retrieves the HardwareProfile resource from the Kubernetes API server.
// This method handles the lookup of hardware profiles with proper error handling for
// common scenarios like missing resources.
//
// The method performs the following operations:
//  1. Constructs a namespaced name for the hardware profile lookup
//  2. Attempts to fetch the resource using the Kubernetes client
//  3. Provides specific error messages for not found vs. other API errors
//
// Error Handling:
//   - Returns a generic error for all errors encountered during the lookup
//
// Parameters:
//   - ctx: Request context for the Kubernetes API call
//   - namespace: The namespace containing the HardwareProfile resource
//   - name: The name of the HardwareProfile resource to fetch
//
// Returns:
//   - *infrav1.HardwareProfile: The fetched HardwareProfile resource
//   - error: Descriptive error for lookup failures, nil on success
func (i *Injector) fetchHardwareProfile(ctx context.Context, namespace, name string) (*infrav1.HardwareProfile, error) {
	hwp := &infrav1.HardwareProfile{}
	key := types.NamespacedName{Name: name, Namespace: namespace}

	if err := i.Client.Get(ctx, key, hwp); err != nil {
		return nil, err
	}

	return hwp, nil
}

// applyHardwareProfileToWorkload applies hardwareprofile specifications to any supported
// Kubernetes workload resource. This method is the central orchestrator for applying
// all hardware profile configurations to workload resources.
//
// The method handles two main categories of hardware profile specifications:
//  1. Resource Requirements: CPU, memory, and custom resource identifiers (e.g., GPUs)
//  2. Scheduling Configuration: Kueue queue assignments and node scheduling constraints
//
// Resource Application Strategy:
//   - Only applies resource requirements to containers that don't already have them
//   - Preserves existing resource specifications in containers
//   - Supports both standard resources (CPU, memory) and custom resources (nvidia.com/gpu, amd.com/gpu)
//
// Scheduling Configuration:
//   - Applies Kueue LocalQueue labels for queue-based scheduling
//   - Applies node scheduling constraints (nodeSelector, tolerations)
//   - When profileChanged is true, clears existing scheduling configuration before applying new settings
//   - When profileChanged is false, merges tolerations to preserve manually-added ones
//
// Parameters:
//   - ctx: Request context containing logger for operation tracking
//   - obj: The unstructured workload object to modify (Notebook, InferenceService, etc.)
//   - hwp: The HardwareProfile resource containing specifications to apply
//   - profileChanged: Whether the hardware profile annotation changed (triggers clearing)
//
// Returns:
//   - []string: Warnings about configuration values that will be overwritten
//   - error: Any error encountered during hardwareprofile application, nil on success
func (i *Injector) applyHardwareProfileToWorkload(ctx context.Context, obj *unstructured.Unstructured, hwp *infrav1.HardwareProfile, profileChanged bool) ([]string, error) {
	log := logf.FromContext(ctx)

	var warnings []string

	// When the hardware profile changes, clear existing scheduling configuration first
	// This ensures stale tolerations/nodeSelector from the old profile are removed
	if profileChanged {
		log.V(1).Info("clearing existing scheduling settings due to profile change",
			"workload", obj.GetName(), "kind", obj.GetKind(), "hardwareProfile", hwp.Name)

		// Remove Kueue label
		resources.RemoveLabel(obj, cluster.KueueQueueNameLabel)

		// Remove nodeSelector and tolerations
		if config, err := GetWorkloadConfig(obj.GetKind()); err == nil {
			unstructured.RemoveNestedField(obj.Object, config.NodeSelectorPath...)
			unstructured.RemoveNestedField(obj.Object, config.TolerationsPath...)
		} else {
			return nil, fmt.Errorf("failed to clear scheduling fields - unsupported workload kind: %s: %w", obj.GetKind(), err)
		}
	}

	log.V(1).Info("applying HWP settings to workload", "workload", obj.GetName(), "kind", obj.GetKind(), "hardwareProfile", hwp.Name)

	// Apply resource requirements to containers (only if there are identifiers)
	if len(hwp.Spec.Identifiers) > 0 {
		if err := i.applyResourceRequirementsToWorkload(obj, hwp); err != nil {
			return nil, fmt.Errorf("failed to apply resource requirements: %w", err)
		}
	}

	// Apply scheduling configuration if present
	if hwp.Spec.SchedulingSpec != nil {
		// Apply Kueue LocalQueue label if .spec.schedulingSpec.kueue.localQueueName is set
		if hwp.Spec.SchedulingSpec.Kueue != nil && hwp.Spec.SchedulingSpec.Kueue.LocalQueueName != "" {
			hwpKueueValue := hwp.Spec.SchedulingSpec.Kueue.LocalQueueName

			// Check if user modified the Kueue label to a different value - warn them it will be overwritten
			// Only warn when the profile hasn't changed (merge behavior), not when switching profiles
			if !profileChanged {
				existingKueueValue := resources.GetLabel(obj, cluster.KueueQueueNameLabel)
				if existingKueueValue != "" && existingKueueValue != hwpKueueValue {
					warnings = append(warnings, fmt.Sprintf(
						"label '%s' has value '%s' which will be overwritten by HardwareProfile '%s' which has value '%s'",
						cluster.KueueQueueNameLabel, existingKueueValue, hwp.Name, hwpKueueValue))
				}
			}

			resources.SetLabel(obj, cluster.KueueQueueNameLabel, hwpKueueValue)
			return warnings, nil // won't need to continue handling Node scheduling configuration
		}

		// Apply Node scheduling configuration if .spec.schedulingSpec.node is set
		if hwp.Spec.SchedulingSpec.Node != nil {
			nodeWarnings, err := i.applyNodeSchedulingConfiguration(obj, hwp.Spec.SchedulingSpec.Node, profileChanged, hwp.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to apply node scheduling configuration: %w", err)
			}
			warnings = append(warnings, nodeWarnings...)
		}
	}

	return warnings, nil
}

// GetWorkloadConfig returns the workload configuration for a given kind.
//
// This function provides access to the workload-specific configuration paths
// that define where containers, nodeSelector, and tolerations are located
// within different Kubernetes resource types.
//
// Parameters:
//   - kind: The Kubernetes resource kind (e.g., "Notebook", "InferenceService")
//
// Returns:
//   - WorkloadConfig: Configuration containing JSON paths for the workload type
//   - error: Error if the workload kind is not supported by the webhook
func GetWorkloadConfig(kind string) (WorkloadConfig, error) {
	config, exists := WorkloadConfigs[kind]
	if !exists {
		return WorkloadConfig{}, fmt.Errorf("unsupported workload kind: %s", kind)
	}
	return config, nil
}

// applyResourceRequirementsToWorkload applies resource requirements (cpu, memory, counts) to all containers
// in a workload resource. This method handles the container-level resource injection
// for both standard and custom resource types.
//
// Parameters:
//   - obj: The unstructured workload object containing containers to modify
//   - hwp: The HardwareProfile resource containing resource identifiers to apply
//
// Returns:
//   - error: Any error encountered during resource requirement application, nil on success

func (i *Injector) applyResourceRequirementsToWorkload(obj *unstructured.Unstructured, hwp *infrav1.HardwareProfile) error {
	config, err := GetWorkloadConfig(obj.GetKind())
	if err != nil {
		return err
	}
	// Handle different workload types explicitly
	switch obj.GetKind() {
	case gvk.InferenceServices.Kind:
		// For InferenceServices, apply resources to the model object
		return i.applyResourceRequirementsToInferenceServiceModel(obj, hwp, config.ContainersPath)
	case gvk.Notebook.Kind:
		// For Notebooks, apply resources to containers
		return i.applyResourceRequirementsToContainers(obj, hwp, config.ContainersPath)
	case gvk.LLMInferenceServiceV1Alpha1.Kind:
		// For LLMInferenceServices, apply resources to containers
		return i.applyResourceRequirementsToContainers(obj, hwp, config.ContainersPath)
	default:
		// This should never happen since isExpectedKind() should catch unsupported kinds earlier
		return fmt.Errorf("unsupported workload kind: %s", obj.GetKind())
	}
}

// for isvc.
func (i *Injector) applyResourceRequirementsToInferenceServiceModel(obj *unstructured.Unstructured, hwp *infrav1.HardwareProfile, modelPath []string) error {
	// Get the model object from the InferenceService
	model, found, err := unstructured.NestedMap(obj.Object, modelPath...)
	if err != nil {
		return fmt.Errorf("failed to get model: %w", err)
	}
	if !found {
		return nil // No model found
	}

	// Apply resource requirements to the model object
	if err := i.applyIdentifiersToContainer(model, hwp.Spec.Identifiers); err != nil {
		return fmt.Errorf("failed to apply resources to model: %w", err)
	}

	// Update the object with modified model
	return unstructured.SetNestedMap(obj.Object, model, modelPath...)
}

// For Notebooks, InferenceServices, and LLMInferenceServices, apply resource requirements to the containers.
func (i *Injector) applyResourceRequirementsToContainers(obj *unstructured.Unstructured, hwp *infrav1.HardwareProfile, containersPath []string) error {
	// Get containers from the workload
	containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
	if err != nil {
		return fmt.Errorf("failed to get containers: %w", err)
	}

	// If no containers found, create the minimal structure needed for resource injection
	if !found || len(containers) == 0 {
		if obj.GetKind() == gvk.LLMInferenceServiceV1Alpha1.Kind {
			// Create minimal container with name "main"
			containers = []interface{}{map[string]interface{}{
				"name": "main",
			}}
		} else { // notebook kind
			return nil
		}
	}

	// Apply resource requirements to each existing container
	for idx, container := range containers {
		if err := i.applyIdentifiersToContainer(container, hwp.Spec.Identifiers); err != nil {
			return fmt.Errorf("failed to apply resources to container %d: %w", idx, err)
		}
	}

	// Update the object with modified containers
	return unstructured.SetNestedSlice(obj.Object, containers, containersPath...)
}

// applyIdentifiersToContainer applies resource requirements to a single container.
// This method implements the granular resource application logic that only adds
// resource requirements for identifiers that don't already exist in the container.
//
// Parameters:
//   - container: The container interface{} to modify (must be map[string]interface{})
//   - identifiers: Array of hardware identifiers to apply from the hardware profile
//
// Returns:
//   - error: Any error encountered during resource application, nil on success
func (i *Injector) applyIdentifiersToContainer(container interface{}, identifiers []infrav1.HardwareIdentifier) error {
	containerMap, ok := container.(map[string]interface{})
	if !ok {
		return errors.New("container is not a map[string]interface{}")
	}

	// Get or create resources section
	resourcesMap, err := webhookutils.GetOrCreateNestedMap(containerMap, "resources")
	if err != nil {
		return err
	}

	// Get or create requests section
	requests, err := webhookutils.GetOrCreateNestedMap(resourcesMap, "requests")
	if err != nil {
		return err
	}

	// For requests - always applies DefaultCount
	if err := i.applyIdentifiersToRequests(requests, identifiers, func(id infrav1.HardwareIdentifier) (intstr.IntOrString, bool) {
		return id.DefaultCount, true
	}); err != nil {
		return err
	}

	// Get or create limits section
	limits, err := webhookutils.GetOrCreateNestedMap(resourcesMap, "limits")
	if err != nil {
		return err
	}

	// For limits - only applies MaxCount if it exists in HWProfile
	if err := i.applyIdentifiersToRequests(limits, identifiers, func(id infrav1.HardwareIdentifier) (intstr.IntOrString, bool) {
		if id.MaxCount == nil {
			return intstr.IntOrString{}, false
		}
		return *id.MaxCount, true
	}); err != nil {
		return err
	}

	// Update modified resources
	resourcesMap["requests"] = requests
	resourcesMap["limits"] = limits
	containerMap["resources"] = resourcesMap
	return nil
}

// applyIdentifiersToRequests applies hardware identifiers to resource requests map.
// This method implements the core logic for selectively adding resource requirements
// while preserving existing specifications.
//
// The method iterates through all hardware identifiers and:
//  1. Checks if the resource identifier already exists in the requests map
//  2. Skips identifiers that are already present (preserving user specifications)
//  3. Converts the hardware profile's default count to a Kubernetes resource quantity
//  4. Adds the resource requirement to the requests map
//
// Parameters:
//   - requests: The container's resource requests map to modify
//   - identifiers: Array of hardware identifiers from the hardware profile
//
// Returns:
//   - error: Any error encountered during identifier application or quantity conversion
func (i *Injector) applyIdentifiersToRequests(
	requests map[string]interface{},
	identifiers []infrav1.HardwareIdentifier,
	valueExtractor func(infrav1.HardwareIdentifier) (intstr.IntOrString, bool),
) error {
	for _, identifier := range identifiers {
		// Skip if the resource identifier already exists
		if _, exists := requests[identifier.Identifier]; exists {
			continue
		}
		value, shouldApply := valueExtractor(identifier)
		if !shouldApply {
			continue
		}
		quantity, err := convertIntOrStringToQuantity(value)
		if err != nil {
			return fmt.Errorf("failed to convert resource quantity for %s: %w", identifier.Identifier, err)
		}
		requests[identifier.Identifier] = quantity.String()
	}
	return nil
}

// applyNodeSchedulingConfiguration applies node scheduling constraints to the workload.
// This method handles the application of nodeSelector and tolerations from the hardware
// profile to ensure workloads are scheduled on appropriate nodes.
//
// The method applies two types of node scheduling constraints:
//  1. NodeSelector: Key-value pairs that must match node labels
//  2. Tolerations: Specifications that allow scheduling on nodes with matching taints
//
// Configuration Application:
//   - Both nodeSelector and tolerations follow the same behavior based on profileChanged:
//   - When profileChanged is true: Replace existing values (clearing already done)
//   - When profileChanged is false: Merge with existing ones to preserve manually-added values
//   - Both configurations are applied only if present in the hardware profile
//
// Parameters:
//   - obj: The unstructured workload object to modify
//   - nodeSpec: The NodeSchedulingSpec resource containing node scheduling specifications
//   - profileChanged: Whether the hardware profile changed (determines merge vs replace behavior)
//   - hwpName: The name of the HardwareProfile (for warning messages)
//
// Returns:
//   - []string: Warnings about nodeSelector values that will be overwritten
//   - error: Any error encountered during node scheduling configuration application
func (i *Injector) applyNodeSchedulingConfiguration(obj *unstructured.Unstructured, nodeSpec *infrav1.NodeSchedulingSpec, profileChanged bool, hwpName string) ([]string, error) {
	config, err := GetWorkloadConfig(obj.GetKind())
	if err != nil {
		return nil, fmt.Errorf("unsupported workload kind for node scheduling: %s", obj.GetKind())
	}

	var warnings []string

	// Apply nodeSelector if present
	if len(nodeSpec.NodeSelector) > 0 {
		// If profile changed, nodeSelector was already cleared, so just set the new one
		// If profile didn't change, merge with existing nodeSelector to preserve manual ones
		if profileChanged {
			if err := unstructured.SetNestedStringMap(obj.Object, nodeSpec.NodeSelector, config.NodeSelectorPath...); err != nil {
				return nil, fmt.Errorf("failed to set nodeSelector: %w", err)
			}
		} else {
			mergedNodeSelector, nodeSelectorWarnings, err := mergeNodeSelector(obj, config.NodeSelectorPath, nodeSpec.NodeSelector, hwpName)
			if err != nil {
				return nil, fmt.Errorf("failed to merge nodeSelector: %w", err)
			}
			warnings = append(warnings, nodeSelectorWarnings...)
			if err := unstructured.SetNestedStringMap(obj.Object, mergedNodeSelector, config.NodeSelectorPath...); err != nil {
				return nil, fmt.Errorf("failed to set merged nodeSelector: %w", err)
			}
		}
	}

	// Apply tolerations if present
	if len(nodeSpec.Tolerations) > 0 {
		// Convert HWP tolerations to unstructured
		hwpTolerations := make([]interface{}, len(nodeSpec.Tolerations))
		for idx, toleration := range nodeSpec.Tolerations {
			tolerationUnstructured, err := resources.ToUnstructured(&toleration)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tolerations to unstructured: %w", err)
			}
			hwpTolerations[idx] = tolerationUnstructured.Object
		}

		// If profile changed, tolerations were already cleared, so just set the new ones
		// If profile didn't change, merge with existing tolerations to preserve manual ones
		if profileChanged {
			if err := unstructured.SetNestedSlice(obj.Object, hwpTolerations, config.TolerationsPath...); err != nil {
				return nil, fmt.Errorf("failed to set tolerations: %w", err)
			}
		} else {
			mergedTolerations, err := mergeTolerations(obj, config.TolerationsPath, hwpTolerations)
			if err != nil {
				return nil, fmt.Errorf("failed to merge tolerations: %w", err)
			}
			if err := unstructured.SetNestedSlice(obj.Object, mergedTolerations, config.TolerationsPath...); err != nil {
				return nil, fmt.Errorf("failed to set merged tolerations: %w", err)
			}
		}
	}

	return warnings, nil
}

// mergeNodeSelector merges HardwareProfile nodeSelector with existing nodeSelector on the workload.
// This preserves manually-added nodeSelector entries while ensuring HardwareProfile ones are applied.
// However, HardwareProfile nodeSelector entries take precedence over existing ones with the same key.
//
// When a user has modified a nodeSelector key that the HardwareProfile also specifies, a warning
// is returned to notify them that their value will be overwritten by the HardwareProfile value.
//
// Parameters:
//   - obj: The unstructured workload object containing existing nodeSelector
//   - nodeSelectorPath: The path to the nodeSelector field in the workload
//   - hwpNodeSelector: The nodeSelector from the HardwareProfile to apply
//   - hwpName: The name of the HardwareProfile (for warning messages)
//
// Returns:
//   - map[string]string: The merged nodeSelector map
//   - []string: Warnings about nodeSelector values that will be overwritten
//   - error: Any error encountered during the merge operation
func mergeNodeSelector(obj *unstructured.Unstructured, nodeSelectorPath []string, hwpNodeSelector map[string]string, hwpName string) (map[string]string, []string, error) {
	// Get existing nodeSelector from the workload
	existingNodeSelector, _, err := unstructured.NestedStringMap(obj.Object, nodeSelectorPath...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get existing nodeSelector: %w", err)
	}

	var warnings []string

	// Start with existing nodeSelector
	merged := make(map[string]string)
	for k, v := range existingNodeSelector {
		merged[k] = v
	}

	// Apply HWP nodeSelector (overwrites existing keys)
	for k, v := range hwpNodeSelector {
		// Check if user has a different value for this key - warn them it will be overwritten
		if existingValue, exists := existingNodeSelector[k]; exists && existingValue != v {
			warnings = append(warnings, fmt.Sprintf(
				"nodeSelector key '%s' has value '%s' which will be overwritten by HardwareProfile '%s' which has value '%s'",
				k, existingValue, hwpName, v))
		}
		merged[k] = v
	}

	return merged, warnings, nil
}

// mergeTolerations merges HardwareProfile tolerations with existing tolerations on the workload.
// HardwareProfile tolerations take precedence over existing ones with the same key.
// This preserves manually-added tolerations while ensuring HardwareProfile tolerations are applied.
//
// Parameters:
//   - obj: The unstructured workload object containing existing tolerations
//   - tolerationsPath: The path to the tolerations field in the workload
//   - hwpTolerations: The tolerations from the HardwareProfile to apply
//
// Returns:
//   - []interface{}: The merged tolerations slice
//   - error: Any error encountered during the merge operation
func mergeTolerations(obj *unstructured.Unstructured, tolerationsPath []string, hwpTolerations []interface{}) ([]interface{}, error) {
	// Get existing tolerations from the workload
	existingTolerations, _, err := unstructured.NestedSlice(obj.Object, tolerationsPath...)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing tolerations: %w", err)
	}

	// Build a set of HWP toleration keys for deduplication
	hwpTolKeys := make(map[string]bool)
	for _, tol := range hwpTolerations {
		if tolMap, ok := tol.(map[string]interface{}); ok {
			hwpTolKeys[TolerationKey(tolMap)] = true
		}
	}

	// Start with HWP tolerations (they take precedence)
	merged := make([]interface{}, 0, len(hwpTolerations)+len(existingTolerations))
	merged = append(merged, hwpTolerations...)

	// Add existing tolerations that don't conflict with HWP ones
	for _, existing := range existingTolerations {
		if existingMap, ok := existing.(map[string]interface{}); ok {
			if !hwpTolKeys[TolerationKey(existingMap)] {
				merged = append(merged, existing)
			}
		}
	}

	return merged, nil
}

// TolerationKey generates a unique key for a toleration based on its key, operator, value, effect,
// and tolerationSeconds. This is used for deduplication when merging tolerations.
// Two tolerations are considered equivalent only if all five fields match.
// Including value and tolerationSeconds prevents accidental removal of user-specified tolerations
// that share the same key/operator/effect but have different values.
//
// Parameters:
//   - tol: The toleration map to generate a key for
//
// Returns:
//   - string: A unique key string for the toleration
func TolerationKey(tol map[string]interface{}) string {
	key, _ := tol["key"].(string)
	operator, _ := tol["operator"].(string)
	value, _ := tol["value"].(string)
	effect, _ := tol["effect"].(string)
	ts := ""
	if v, ok := tol["tolerationSeconds"]; ok {
		ts = fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf("%s:%s:%s:%s:%s", key, operator, value, effect, ts)
}

// convertIntOrStringToQuantity converts an IntOrString value to a Kubernetes resource.Quantity.
// This utility function handles the conversion of hardware profile resource counts to
// the proper Kubernetes resource quantity format.
//
// Parameters:
//   - value: The IntOrString value from the hardware profile to convert
//
// Returns:
//   - resource.Quantity: The converted Kubernetes resource quantity
//   - error: Any error encountered during conversion or parsing
func convertIntOrStringToQuantity(value intstr.IntOrString) (resource.Quantity, error) {
	switch value.Type {
	case intstr.Int:
		return *resource.NewQuantity(int64(value.IntVal), resource.DecimalSI), nil
	case intstr.String:
		return resource.ParseQuantity(value.StrVal)
	default:
		return resource.Quantity{}, fmt.Errorf("invalid IntOrString type: %v", value.Type)
	}
}
