//go:build !nowebhook

package hardwareprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
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

	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
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
	ContainersPath   []string
	NodeSelectorPath []string
	TolerationsPath  []string
}

// WorkloadConfigs maps Kubernetes resource kinds to their configuration paths.
var WorkloadConfigs = map[string]WorkloadConfig{
	gvk.Notebook.Kind: {
		ContainersPath:   []string{"spec", "template", "spec", "containers"},
		NodeSelectorPath: []string{"spec", "template", "spec", "nodeSelector"},
		TolerationsPath:  []string{"spec", "template", "spec", "tolerations"},
	},
	gvk.InferenceServices.Kind: {
		ContainersPath:   []string{"spec", "predictor", "model"},
		NodeSelectorPath: []string{"spec", "predictor", "nodeSelector"},
		TolerationsPath:  []string{"spec", "predictor", "tolerations"},
	},
}

//+kubebuilder:webhook:path=/mutate-hardware-profile,mutating=true,failurePolicy=fail,groups=kubeflow.org,resources=notebooks,verbs=create;update,versions=v1,name=hardwareprofile-notebook-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/mutate-hardware-profile,mutating=true,failurePolicy=fail,groups=serving.kserve.io,resources=inferenceservices,verbs=create;update,versions=v1beta1,name=hardwareprofile-kserve-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

// Injector implements a mutating admission webhook for hardware profile injection.
type Injector struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
}

// Assert that Injector implements admission.Handler interface.
var _ admission.Handler = &Injector{}

// SetupWithManager registers the validating webhook with the provided controller-runtime manager.
//
// Parameters:
//   - mgr: The controller-runtime manager to register the webhook with.
//
// Returns:
//   - error: Always nil (for future extensibility).
func (i *Injector) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()

	// Register single webhook path for both Notebooks and InferenceServices
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

	// Decode the object to check deletion timestamp
	obj := &unstructured.Unstructured{}
	if err := i.Decoder.Decode(req, obj); err != nil {
		log.Error(err, "Failed to decode object")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Skip processing if object is marked for deletion
	if !obj.GetDeletionTimestamp().IsZero() {
		return admission.Allowed("Object marked for deletion, skipping hardware profile injection")
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		resp = i.performHardwareProfileInjection(ctx, &req, obj)
	default:
		resp = admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
	}

	return resp
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
		gvk.Notebook,          // kubeflow.org/v1/Notebook
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

// performHardwareProfileInjection handles the core logic for hardware profile injection.
// This method orchestrates the entire process of applying hardware profile specifications
// to workload resources.
//
// The injection process follows these steps:
//  1. Decode the workload object from the admission request
//  2. Check for hardware profile annotations on the object
//  3. Determine the namespace for the hardware profile lookup
//  4. Fetch the HardwareProfile resource from the Kubernetes API
//  5. Validate that the hardware profile has meaningful configuration
//  6. Set the hardware profile namespace annotation if not present
//  7. Apply hardware profile specifications to the workload
//  8. Return the modified object as a patch response
//
// Annotation Handling:
//   - opendatahub.io/hardware-profile-name: Required annotation specifying the profile name
//   - opendatahub.io/hardware-profile-namespace: Optional annotation for cross-namespace profiles
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

	// Get the hardware profile
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

	// Early exit if hardware profile has no meaningful configuration
	if len(hwp.Spec.Identifiers) == 0 && hwp.Spec.SchedulingSpec == nil {
		return admission.Allowed("Hardware profile has no configuration to apply")
	}

	// Set the hardware profile namespace annotation
	resources.SetAnnotation(obj, HardwareProfileNamespaceAnnotation, profileNamespace)

	// Apply hardware profile specifications
	if err := i.applyHardwareProfileToWorkload(ctx, obj, hwp); err != nil {
		log.Error(err, "Failed to apply hardware profile", "profile", profileName)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Marshal the modified object
	marshaledObj, err := json.Marshal(obj)
	if err != nil {
		log.Error(err, "Failed to marshal modified object")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Return the admission response with the modified object
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
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
//   - Returns a descriptive error for non-existent hardware profiles (404)
//   - Returns a generic error for other API failures (network, permissions, etc.)
//
// Parameters:
//   - ctx: Request context for the Kubernetes API call
//   - namespace: The namespace containing the HardwareProfile resource
//   - name: The name of the HardwareProfile resource to fetch
//
// Returns:
//   - *hwpv1alpha1.HardwareProfile: The fetched HardwareProfile resource
//   - error: Descriptive error for lookup failures, nil on success
func (i *Injector) fetchHardwareProfile(ctx context.Context, namespace, name string) (*hwpv1alpha1.HardwareProfile, error) {
	hwp := &hwpv1alpha1.HardwareProfile{}
	key := types.NamespacedName{Name: name, Namespace: namespace}

	if err := i.Client.Get(ctx, key, hwp); err != nil {
		return nil, err
	}

	return hwp, nil
}

// applyHardwareProfileToWorkload applies hardware profile specifications to any supported
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
//   - Supports both standard resources (CPU, memory) and custom resources (nvidia.com/gpu)
//
// Scheduling Configuration:
//   - Applies Kueue LocalQueue labels for queue-based scheduling
//   - Applies node scheduling constraints (nodeSelector, tolerations)
//   - Always applies scheduling configuration regardless of existing values
//
// Parameters:
//   - ctx: Request context containing logger for operation tracking
//   - obj: The unstructured workload object to modify (Notebook, InferenceService, etc.)
//   - hwp: The HardwareProfile resource containing specifications to apply
//
// Returns:
//   - error: Any error encountered during hardware profile application, nil on success
func (i *Injector) applyHardwareProfileToWorkload(ctx context.Context, obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile) error {
	log := logf.FromContext(ctx)

	log.V(1).Info("applying hardware profile to workload", "workload", obj.GetName(), "kind", obj.GetKind(), "hardwareProfile", hwp.Name)

	// Apply resource requirements to containers (only if there are identifiers)
	if len(hwp.Spec.Identifiers) > 0 {
		if err := i.applyResourceRequirementsToWorkload(obj, hwp); err != nil {
			return fmt.Errorf("failed to apply resource requirements: %w", err)
		}
	}

	// Apply scheduling configuration if present
	if hwp.Spec.SchedulingSpec != nil {
		// Apply Kueue LocalQueue label
		if hwp.Spec.SchedulingSpec.Kueue != nil && hwp.Spec.SchedulingSpec.Kueue.LocalQueueName != "" {
			resources.SetLabel(obj, cluster.KueueQueueNameLabel, hwp.Spec.SchedulingSpec.Kueue.LocalQueueName)
		}

		// Apply Node scheduling configuration
		if hwp.Spec.SchedulingSpec.Node != nil {
			if err := i.applyNodeSchedulingConfiguration(obj, hwp); err != nil {
				return fmt.Errorf("failed to apply node scheduling configuration: %w", err)
			}
		}
	}

	return nil
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

// applyResourceRequirementsToWorkload applies resource requirements to all containers
// in a workload resource. This method handles the container-level resource injection
// for both standard and custom resource types.
//
// Parameters:
//   - obj: The unstructured workload object containing containers to modify
//   - hwp: The HardwareProfile resource containing resource identifiers to apply
//
// Returns:
//   - error: Any error encountered during resource requirement application, nil on success

func (i *Injector) applyResourceRequirementsToWorkload(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile) error {
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
	default: // TODO: add llmisvc
		// For any other workload types, apply resources to containers as default behavior
		return i.applyResourceRequirementsToContainers(obj, hwp, config.ContainersPath)
	}
}

// for isvc.
func (i *Injector) applyResourceRequirementsToInferenceServiceModel(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile, modelPath []string) error {
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

// for notebooks.
func (i *Injector) applyResourceRequirementsToContainers(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile, containersPath []string) error {
	// Get containers from the workload
	containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
	if err != nil {
		return fmt.Errorf("failed to get containers: %w", err)
	}
	if !found || len(containers) == 0 {
		return nil // No containers found
	}

	// Apply resource requirements to each container
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
func (i *Injector) applyIdentifiersToContainer(container interface{}, identifiers []hwpv1alpha1.HardwareIdentifier) error {
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
	if err := i.applyIdentifiersToRequests(requests, identifiers, func(id hwpv1alpha1.HardwareIdentifier) (intstr.IntOrString, bool) {
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
	if err := i.applyIdentifiersToRequests(limits, identifiers, func(id hwpv1alpha1.HardwareIdentifier) (intstr.IntOrString, bool) {
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
	identifiers []hwpv1alpha1.HardwareIdentifier,
	valueExtractor func(hwpv1alpha1.HardwareIdentifier) (intstr.IntOrString, bool),
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
//   - NodeSelector is applied as a complete replacement of existing values
//   - Tolerations are applied as a complete replacement of existing values
//   - Both configurations are applied only if present in the hardware profile
//
// Parameters:
//   - obj: The unstructured workload object to modify
//   - hwp: The HardwareProfile resource containing node scheduling specifications
//
// Returns:
//   - error: Any error encountered during node scheduling configuration application
func (i *Injector) applyNodeSchedulingConfiguration(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile) error {
	nodeSpec := hwp.Spec.SchedulingSpec.Node

	config, err := GetWorkloadConfig(obj.GetKind())
	if err != nil {
		return fmt.Errorf("unsupported workload kind for node scheduling: %s", obj.GetKind())
	}

	// Apply nodeSelector if present
	if len(nodeSpec.NodeSelector) > 0 {
		if err := unstructured.SetNestedStringMap(obj.Object, nodeSpec.NodeSelector, config.NodeSelectorPath...); err != nil {
			return fmt.Errorf("failed to set nodeSelector: %w", err)
		}
	}

	// Apply tolerations if present
	if len(nodeSpec.Tolerations) > 0 {
		tolerationsSlice := make([]interface{}, len(nodeSpec.Tolerations))
		for i, toleration := range nodeSpec.Tolerations {
			tolerationUnstructured, err := resources.ToUnstructured(&toleration)
			if err != nil {
				return fmt.Errorf("failed to convert toleration to unstructured: %w", err)
			}
			tolerationsSlice[i] = tolerationUnstructured.Object
		}

		if err := unstructured.SetNestedSlice(obj.Object, tolerationsSlice, config.TolerationsPath...); err != nil {
			return fmt.Errorf("failed to set tolerations: %w", err)
		}
	}

	return nil
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
