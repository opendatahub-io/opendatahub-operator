//go:build !nowebhook

// Package hardwareprofile implements a mutating admission webhook for injecting HardwareProfile
// specifications into workload resources.
//
// The webhook intercepts workload creation and update requests and looks for the annotation:
//   - opendatahub.io/hardware-profile-name: The name of the HardwareProfile to apply
//
// When this annotation is found, the webhook:
//  1. Fetches the specified HardwareProfile from the Kubernetes API
//  2. Applies CPU, memory, and GPU resource requirements to containers
//  3. Applies Kueue LocalQueue name as a label
//  4. Sets the hardware profile namespace annotation
//
// This implementation supports Kubeflow Notebook CRDs, which are the primary workload type
// created by the ODH Dashboard.
package hardwareprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/shared"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// Annotation constants.
const (
	HardwareProfileNameAnnotation      = "opendatahub.io/hardware-profile-name"
	HardwareProfileNamespaceAnnotation = "opendatahub.io/hardware-profile-namespace"
	KueueLocalQueueLabel               = "kueue.x-k8s.io/queue-name"
)

// Common field paths for unstructured operations.
var (
	containersPath   = []string{"spec", "template", "spec", "containers"}
	nodeSelectorPath = []string{"spec", "template", "spec", "nodeSelector"}
	tolerationsPath  = []string{"spec", "template", "spec", "tolerations"}
)

//+kubebuilder:webhook:path=/mutate-kubeflow-notebook,mutating=true,failurePolicy=fail,groups=kubeflow.org,resources=notebooks,verbs=create;update,versions=v1,name=hardwareprofile-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

// Injector implements a mutating admission webhook for hardware profile injection.
// It intercepts Notebook CRD creation/updates and applies hardware profile specifications.
type Injector struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
}

// Assert that Injector implements admission.Handler interface.
var _ admission.Handler = &Injector{}

// InjectDecoder implements admission.DecoderInjector so the manager can inject the decoder automatically.
//
// Parameters:
//   - d: The admission.Decoder to inject.
//
// Returns:
//   - error: Always nil.
func (i *Injector) InjectDecoder(d admission.Decoder) error {
	i.Decoder = d
	return nil
}

// SetupWithManager registers the mutating webhook with the provided controller-runtime manager.
//
// Parameters:
//   - mgr: The controller-runtime manager to register the webhook with.
//
// Returns:
//   - error: Any error encountered during webhook registration.
func (i *Injector) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/mutate-kubeflow-notebook", &webhook.Admission{
		Handler:        i,
		LogConstructor: shared.NewLogConstructor(i.Name),
	})
	return nil
}

// Handle processes admission requests for workload resources with hardware profile annotations.
// It applies hardware profile specifications to supported workload types.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - req: The admission.Request containing the operation and object details.
//
// Returns:
//   - admission.Response: The result of the admission check with any mutations applied.
func (i *Injector) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Decode the object from the request
	obj := &unstructured.Unstructured{}
	if err := i.Decoder.Decode(req, obj); err != nil {
		log.Error(err, "Failed to decode object")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if the object has hardware profile annotations
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return admission.Allowed("No annotations found")
	}

	profileName := annotations[HardwareProfileNameAnnotation]
	if profileName == "" {
		return admission.Allowed("No hardware profile annotation found")
	}

	// Determine the namespace for the hardware profile
	profileNamespace := annotations[HardwareProfileNamespaceAnnotation]
	if profileNamespace == "" {
		profileNamespace = obj.GetNamespace()
	}

	// Get the hardware profile
	hwp := &hwpv1alpha1.HardwareProfile{}
	if err := i.Client.Get(ctx, client.ObjectKey{Name: profileName, Namespace: profileNamespace}, hwp); err != nil {
		log.Error(err, "Failed to get hardware profile", "profile", profileName, "namespace", profileNamespace)
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to get hardware profile '%s' in namespace '%s': %w", profileName, profileNamespace, err))
	}

	// Set the hardware profile namespace annotation if it wasn't already set
	if annotations[HardwareProfileNamespaceAnnotation] == "" {
		annotations[HardwareProfileNamespaceAnnotation] = profileNamespace
		obj.SetAnnotations(annotations)
	}

	// Apply hardware profile specifications
	if err := i.applyHardwareProfileToWorkload(ctx, obj, profileName, annotations); err != nil {
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

// applyHardwareProfileToWorkload applies hardware profile to any Kubernetes workload with pod template spec.
// This function is generic and works with any resource that follows the pod template pattern
// (e.g., Notebooks, Jobs, Deployments, ReplicaSets, etc.).
//
// Parameters:
//   - ctx: Context for the operation (logger is extracted from here).
//   - obj: The workload object to modify.
//   - hardwareProfileName: The name of the HardwareProfile to apply.
//   - annotations: The annotations map from the object (to avoid re-fetching).
//
// Returns:
//   - error: Any error encountered during hardware profile application.
func (i *Injector) applyHardwareProfileToWorkload(ctx context.Context, obj *unstructured.Unstructured, hardwareProfileName string, annotations map[string]string) error {
	log := logf.FromContext(ctx)

	// Determine which namespace to fetch the hardware profile from
	hardwareProfileNamespace := obj.GetNamespace() // Default to workload namespace
	if annotations != nil {
		if nsAnnotation, exists := annotations[HardwareProfileNamespaceAnnotation]; exists && nsAnnotation != "" {
			hardwareProfileNamespace = nsAnnotation
		}
	}

	// Fetch the HardwareProfile
	hwp, err := i.fetchHardwareProfile(ctx, hardwareProfileNamespace, hardwareProfileName)
	if err != nil {
		return fmt.Errorf("failed to get hardware profile %s: %w", hardwareProfileName, err)
	}

	log.Info("applying hardware profile to workload", "workload", obj.GetName(), "kind", obj.GetKind(), "hardwareProfile", hwp.Name)

	// Apply resource requirements to containers (only if there are identifiers)
	if len(hwp.Spec.Identifiers) > 0 {
		if err := i.applyResourceRequirementsToWorkload(obj, hwp); err != nil {
			return fmt.Errorf("failed to apply resource requirements: %w", err)
		}
	}

	// Apply Kueue LocalQueue label
	i.applyKueueConfiguration(obj, hwp)

	// Apply Node scheduling configuration
	if err := i.applyNodeSchedulingConfiguration(obj, hwp); err != nil {
		return fmt.Errorf("failed to apply node scheduling configuration: %w", err)
	}

	// Set hardware profile namespace annotation
	i.setHardwareProfileNamespaceAnnotation(obj, hwp)

	return nil
}

// fetchHardwareProfile retrieves the HardwareProfile from the API server.
//
// Parameters:
//   - ctx: Context for the API call.
//   - namespace: The namespace to fetch the HardwareProfile from.
//   - name: The name of the HardwareProfile to fetch.
//
// Returns:
//   - *hwpv1alpha1.HardwareProfile: The fetched HardwareProfile.
//   - error: Any error encountered during the fetch operation.
func (i *Injector) fetchHardwareProfile(ctx context.Context, namespace, name string) (*hwpv1alpha1.HardwareProfile, error) {
	hwp := &hwpv1alpha1.HardwareProfile{}
	key := types.NamespacedName{Name: name, Namespace: namespace}

	if err := i.Client.Get(ctx, key, hwp); err != nil {
		if k8serr.IsNotFound(err) {
			return nil, fmt.Errorf("hardware profile %s not found in namespace %s", name, namespace)
		}
		return nil, fmt.Errorf("failed to get hardware profile %s: %w", name, err)
	}

	return hwp, nil
}

// applyResourceRequirementsToWorkload applies resource requirements to workload containers.
// This function works with any Kubernetes workload that has containers in spec.template.spec.containers.
//
// Parameters:
//   - obj: The workload object to modify.
//   - hwp: The HardwareProfile containing resource specifications.
//
// Returns:
//   - error: Any error encountered during resource requirement application.
func (i *Injector) applyResourceRequirementsToWorkload(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile) error {
	// Navigate to spec.template.spec.containers
	containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
	if err != nil {
		return fmt.Errorf("failed to get containers: %w", err)
	}
	if !found || len(containers) == 0 {
		// No containers found - nothing to modify
		return nil
	}

	// Apply resource requirements to each container
	for idx, container := range containers {
		if err := i.applyResourceRequirementsToContainer(container, hwp.Spec.Identifiers); err != nil {
			return fmt.Errorf("failed to apply resources to container %d: %w", idx, err)
		}
	}

	// Update the object with modified containers
	return unstructured.SetNestedSlice(obj.Object, containers, containersPath...)
}

// applyKueueConfiguration applies Kueue LocalQueue configuration to the workload.
// This function works with any Kubernetes workload by setting the appropriate label.
//
// Parameters:
//   - obj: The workload object to modify.
//   - hwp: The HardwareProfile containing Kueue specifications.
func (i *Injector) applyKueueConfiguration(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile) {
	if hwp.Spec.SchedulingSpec == nil || hwp.Spec.SchedulingSpec.Kueue == nil {
		return // No Kueue configuration to apply
	}

	localQueueName := hwp.Spec.SchedulingSpec.Kueue.LocalQueueName
	if localQueueName == "" {
		return // No queue name to apply
	}

	// Apply LocalQueue name as a label on the workload metadata using resources utility
	resources.SetLabel(obj, KueueLocalQueueLabel, localQueueName)
}

// applyNodeSchedulingConfiguration applies node scheduling configuration to the workload.
// This function works with any Kubernetes workload that has a pod template spec.
//
// Parameters:
//   - obj: The workload object to modify.
//   - hwp: The HardwareProfile containing node scheduling specifications.
//
// Returns:
//   - error: Any error encountered during node scheduling configuration application.
func (i *Injector) applyNodeSchedulingConfiguration(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile) error {
	if hwp.Spec.SchedulingSpec == nil || hwp.Spec.SchedulingSpec.Node == nil {
		return nil // No node scheduling configuration to apply
	}

	nodeSpec := hwp.Spec.SchedulingSpec.Node

	// Apply nodeSelector to spec.template.spec.nodeSelector
	if len(nodeSpec.NodeSelector) > 0 {
		if err := unstructured.SetNestedStringMap(obj.Object, nodeSpec.NodeSelector, nodeSelectorPath...); err != nil {
			return fmt.Errorf("failed to set nodeSelector: %w", err)
		}
	}

	// Apply tolerations to spec.template.spec.tolerations
	if len(nodeSpec.Tolerations) > 0 {
		// Convert each toleration to unstructured format using resources helper
		tolerationsSlice := make([]interface{}, len(nodeSpec.Tolerations))
		for i, toleration := range nodeSpec.Tolerations {
			tolerationUnstructured, err := resources.ToUnstructured(&toleration)
			if err != nil {
				return fmt.Errorf("failed to convert toleration to unstructured: %w", err)
			}
			tolerationsSlice[i] = tolerationUnstructured.Object
		}

		if err := unstructured.SetNestedSlice(obj.Object, tolerationsSlice, tolerationsPath...); err != nil {
			return fmt.Errorf("failed to set tolerations: %w", err)
		}
	}

	return nil
}

// applyResourceRequirementsToContainer applies resource requirements to a single container.
//
// Parameters:
//   - container: The container object (interface{}) to modify.
//   - identifiers: The hardware identifiers to apply.
//
// Returns:
//   - error: Any error encountered during resource application.
func (i *Injector) applyResourceRequirementsToContainer(container interface{}, identifiers []hwpv1alpha1.HardwareIdentifier) error {
	// Each container should be a map[string]interface{} representing a Kubernetes Container object
	containerMap, ok := container.(map[string]interface{})
	if !ok {
		return nil // Skip non-map containers
	}

	// If no identifiers to apply, skip processing but don't fail
	if len(identifiers) == 0 {
		return nil
	}

	// Get or create resources section
	resourcesMap, err := getOrCreateNestedMap(containerMap, "resources")
	if err != nil {
		return err
	}

	// Get or create requests section
	requests, err := getOrCreateNestedMap(resourcesMap, "requests")
	if err != nil {
		return err
	}

	// Apply hardware profile resource requirements
	for _, identifier := range identifiers {
		// Skip empty identifiers
		if identifier.Identifier == "" {
			continue
		}

		// Use DefaultCount for resource requests
		quantity, err := convertIntOrStringToQuantity(identifier.DefaultCount)
		if err != nil {
			return fmt.Errorf("failed to convert resource quantity for %s: %w", identifier.Identifier, err)
		}
		requests[identifier.Identifier] = quantity.String()
	}

	// Update the container with new resource requirements
	// Since we're working with maps, we can directly assign them
	resourcesMap["requests"] = requests
	containerMap["resources"] = resourcesMap

	return nil
}

// getOrCreateNestedMap safely gets or creates a nested map in an unstructured object.
//
// Parameters:
//   - obj: The parent map object.
//   - field: The field name to get or create.
//
// Returns:
//   - map[string]interface{}: The nested map.
//   - error: Any error encountered.
func getOrCreateNestedMap(obj map[string]interface{}, field string) (map[string]interface{}, error) {
	nested, found, err := unstructured.NestedMap(obj, field)
	if err != nil {
		return nil, fmt.Errorf("failed to get nested map for field %s: %w", field, err)
	}
	if !found {
		nested = make(map[string]interface{})
	}
	return nested, nil
}

// setHardwareProfileNamespaceAnnotation sets the hardware profile namespace annotation.
// This function works with any Kubernetes workload.
//
// Parameters:
//   - obj: The workload object to modify.
//   - hwp: The HardwareProfile containing namespace information.
func (i *Injector) setHardwareProfileNamespaceAnnotation(obj *unstructured.Unstructured, hwp *hwpv1alpha1.HardwareProfile) {
	// Use the resources utility function for cleaner annotation management
	resources.SetAnnotation(obj, HardwareProfileNamespaceAnnotation, hwp.Namespace)
}

// convertIntOrStringToQuantity converts an IntOrString value to a resource.Quantity.
//
// Parameters:
//   - value: The IntOrString value to convert.
//
// Returns:
//   - resource.Quantity: The converted quantity.
//   - error: Any error encountered during conversion.
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
