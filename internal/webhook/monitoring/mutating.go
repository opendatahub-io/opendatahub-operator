//go:build !nowebhook

package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/mutate-prometheus-monitors,mutating=true,failurePolicy=fail,groups=monitoring.coreos.com,resources=podmonitors,verbs=create;update,versions=v1,name=podmonitor-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//+kubebuilder:webhook:path=/mutate-prometheus-monitors,mutating=true,failurePolicy=fail,groups=monitoring.coreos.com,resources=servicemonitors,verbs=create;update,versions=v1,name=servicemonitor-injector.opendatahub.io,sideEffects=None,admissionReviewVersions=v1
//nolint:lll

// Injector implements a mutating admission webhook for monitoring injection.
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

	// Register the webhook path (must match the path in kubebuilder annotations)
	hookServer.Register("/mutate-prometheus-monitors", &webhook.Admission{
		Handler:        i,
		LogConstructor: webhookutils.NewWebhookLogConstructor(i.Name),
	})

	return nil
}

// Handle processes admission requests for monitoring-related resources.
// This is the main entry point for the webhook.
//// injection process.
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
		return admission.Allowed("Object marked for deletion, skipping monitoring injection")
	}

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		return i.performMonitoringInjection(ctx, &req, obj)
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
	expectedGVKs := []schema.GroupVersionKind{
		gvk.CoreosServiceMonitor,	// monitoring.coreos.io/v1/ServiceMonitor
		gvk.CoreosPodMonitor,		// monitoring.coreos.io/v1/PodMonitor
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

// performMonitoringInjection handles the core logic for monitoring configuration injection.
//
// Parameters:
//   - ctx: Request context containing logger and other contextual information
//   - req: The admission.Request containing the workload object and operation details
//   - obj: The decoded unstructured object to mutate
//
// Returns:
//   - admission.Response: Success response with object patch or error response with details
func (i *Injector) performMonitoringInjection(ctx context.Context, req *admission.Request, obj *unstructured.Unstructured) admission.Response {
        log := logf.FromContext(ctx)

        resourceNamespace := obj.GetNamespace()
        if resourceNamespace == "" {
                return admission.Errored(http.StatusBadRequest, errors.New("unable to determine resource namespace"))
        }

        ns := &corev1.Namespace{}
        if err := i.Client.Get(ctx, types.NamespacedName{Name: resourceNamespace}, ns); err != nil {
                if k8serr.IsNotFound(err) {
        log.V(1).Info("Namespace not found", "namespace", resourceNamespace)
                        return admission.Errored(http.StatusBadRequest, fmt.Errorf("namespace '%s' not found", resourceNamespace))
                }
                log.Error(err, "Failed to get namespace", "namespace", resourceNamespace)
                return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to get namespace '%s': %w", resourceNamespace, err))
        }

        namespaceLabels := ns.GetLabels()

        if isOpendatahubNamespace, exists := namespaceLabels["opendatahub.io/dashboard"]; exists {
                if isOpendatahubNamespace != "true" {
                        log.V(1).Info("Ignore non odh namespace", "namespace", resourceNamespace)
			return admission.Allowed("ignored")
                }
        }

        if isMonitoredNamespace, exists := namespaceLabels["opendatahub.io/monitoring"]; exists {
		if isMonitoredNamespace == "true" {
                        log.V(1).Info("Performing monitoring injection",
                		"resource", obj.GetName(),
                		"namespace", resourceNamespace,
                		"labels", namespaceLabels)

			// Inject opendatahub.io/monitoring=true label
			labels := obj.GetLabels()
			if labels == nil {
				labels = make(map[string]string)
			}
			labels["opendatahub.io/monitoring"] = "true"
			obj.SetLabels(labels)

			// Marshal the modified object
			marshaledObj, err := json.Marshal(obj)
			if err != nil {
				log.Error(err, "Failed to marshal modified object")
				return admission.Errored(http.StatusInternalServerError, err)
			}

			// Return the admission response with the modified object
			return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
		}
        }

	log.V(1).Info("Namespace not labeled for monitoring", "namespace", resourceNamespace)
        return admission.Allowed("Namespace not configured for monitoring")
}


