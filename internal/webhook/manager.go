//go:build !nowebhook

package webhook

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	gvk "github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// Webhooks for validating:
// - kubeflow.org/v1: pytorchjobs, notebooks
// - ray.io/v1: rayjobs, rayclusters
// - serving.kserve.io/v1beta1: inferenceservices

const (
	WebhookServiceName                 = "opendatahub-operator-webhook-service"
	ValidatingWebhookConfigurationName = "opendatahub-operator-validating-webhook-configuration"
	AdmissionReviewVersion             = "v1"
	WebhookManagerName                 = "WebhookManager"
)

const (
	// Webhook names.
	KserveKueuelabelsValidatorName   = "kserve-kueuelabels-validator.opendatahub.io"
	KubeflowKueuelabelsValidatorName = "kubeflow-kueuelabels-validator.opendatahub.io"
	RayKueuelabelsValidatorName      = "ray-kueuelabels-validator.opendatahub.io"
)

const (
	// InjectCabundleAnnotation is the annotation to inject the CA bundle into the webhook.
	InjectCabundleAnnotation = "service.beta.openshift.io/inject-cabundle"
	// KueueManagedLabelKey indicates a namespace is managed by Kueue.
	KueueManagedLabelKey = "kueue.openshift.io/managed"
	// KueueLegacyManagedLabelKey is the legacy label key used to indicate a namespace is managed by Kueue.
	KueueLegacyManagedLabelKey = "kueue-managed"
)

func newWebhookClientConfigForEnvtest(path string, localPort string, localCertDir string) admissionregistrationv1.WebhookClientConfig {
	// Detected running under envtest/integration tests
	// Read the CA bundle from the local cert directory
	certPath := filepath.Join(localCertDir, "tls.crt")
	cert, err := os.ReadFile(certPath)
	if err != nil {
		panic(fmt.Sprintf("failed to read webhook cert at %s: %v", certPath, err))
	}

	// Construct the URL with the local port and path
	url := "https://" + net.JoinHostPort("localhost", localPort) + path
	return admissionregistrationv1.WebhookClientConfig{
		URL:      &url,
		CABundle: cert,
	}
}

// newWebhookClientConfig creates a ClientConfig for the webhook.
//
// Parameters:
//   - path: The path to the webhook
//   - namespace: The namespace where the webhook is located
//
// Returns:
//   - admissionregistrationv1.WebhookClientConfig: The ClientConfig for the webhook
func newWebhookClientConfig(
	path string,
	namespace string,
) admissionregistrationv1.WebhookClientConfig {
	// Envtest mode: use URL with CA bundle
	localPort := os.Getenv("ENVTEST_WEBHOOK_LOCAL_PORT")
	localCertDir := os.Getenv("ENVTEST_WEBHOOK_LOCAL_CERT_DIR")

	if localPort != "" && localCertDir != "" {
		return newWebhookClientConfigForEnvtest(path, localPort, localCertDir)
	}

	// Production mode: use ServiceRef with CA injection
	return admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{
			Name:      WebhookServiceName,
			Namespace: namespace,
			Path:      &path,
		},
	}
}

// newValidatingWebhook creates a base ValidatingWebhook object.
//
// Parameters:
//   - name: The name of the webhook
//   - namespace: The namespace where the webhook is located
//   - path: The path to the webhook
//   - rules: The rules for the webhook
//   - namespaceSelector: The namespace selector for the webhook
//
// Returns:
//   - admissionregistrationv1.ValidatingWebhook: The ValidatingWebhook object
func newValidatingWebhook(
	name string,
	namespace string,
	path string,
	rules []admissionregistrationv1.RuleWithOperations,
	namespaceSelector *metav1.LabelSelector,
) admissionregistrationv1.ValidatingWebhook {
	sideEffects := admissionregistrationv1.SideEffectClassNone
	failurePolicyFail := admissionregistrationv1.Fail
	return admissionregistrationv1.ValidatingWebhook{
		Name:                    name,
		AdmissionReviewVersions: []string{AdmissionReviewVersion},
		ClientConfig:            newWebhookClientConfig(path, namespace),
		FailurePolicy:           &failurePolicyFail,
		Rules:                   rules,
		SideEffects:             &sideEffects,
		NamespaceSelector:       namespaceSelector,
	}
}

// getDesiredKueueValidatingWebhooks defines the Kueue-related validating webhooks.
// This implements the OR logic for namespace selection by creating multiple webhook entries explicitly.
// Parameters:
//   - namespace: The namespace where the webhook is located
//   - labelKey: The label key to use for the namespace selector (e.g., KueueManagedLabelKey or KueueManagedOldLabelKey)
//   - suffix: A suffix to append to the webhook name for uniqueness "-legacy"
//
// Returns:
//   - []admissionregistrationv1.ValidatingWebhook: A slice of ValidatingWebhook objects for Kueue
//
// caBundle parameter has been removed as per your request.
func getDesiredKueueValidatingWebhooks(namespace string, labelKey string, suffix string) []admissionregistrationv1.ValidatingWebhook {
	return []admissionregistrationv1.ValidatingWebhook{
		newValidatingWebhook(
			KserveKueuelabelsValidatorName+suffix,
			namespace,
			kueuewebhook.ValidateKueuePath,
			[]admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{gvk.InferenceServices.Group},
						APIVersions: []string{gvk.InferenceServices.Version},
						Resources:   []string{"inferenceservices"},
					},
				},
			},
			&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: []string{"true"}},
				},
			},
		),

		newValidatingWebhook(
			KubeflowKueuelabelsValidatorName+suffix,
			namespace,
			kueuewebhook.ValidateKueuePath,
			[]admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{gvk.Notebook.Group},
						APIVersions: []string{gvk.Notebook.Version},
						Resources:   []string{"pytorchjobs", "notebooks"},
					},
				},
			},
			&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: []string{"true"}},
				},
			},
		),

		newValidatingWebhook(
			RayKueuelabelsValidatorName+suffix,
			namespace,
			kueuewebhook.ValidateKueuePath,
			[]admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{gvk.Ray.Group},
						APIVersions: []string{gvk.Ray.Version},
						Resources:   []string{"rayjobs", "rayclusters"},
					},
				},
			},
			&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: []string{"true"}},
				},
			},
		),
	}
}

// DesiredValidatingWebhookConfiguration defines the desired state of the ValidatingWebhookConfiguration.
//
// Parameters:
//   - namespace: The namespace where the webhook is located
//
// Returns:
//   - *admissionregistrationv1.ValidatingWebhookConfiguration: The ValidatingWebhookConfiguration object
func DesiredValidatingWebhookConfiguration(namespace string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{
		// TypeMeta is required for the SSA
		TypeMeta: metav1.TypeMeta{
			Kind:       "ValidatingWebhookConfiguration",
			APIVersion: "admissionregistration.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: ValidatingWebhookConfigurationName,
			Annotations: map[string]string{
				InjectCabundleAnnotation: "true",
			},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{},
	}

	vwc.Webhooks = append(vwc.Webhooks, getDesiredKueueValidatingWebhooks(namespace, KueueManagedLabelKey, "")...)
	// TODO: Remove this once we drop support for the legacy label
	vwc.Webhooks = append(vwc.Webhooks, getDesiredKueueValidatingWebhooks(namespace, KueueLegacyManagedLabelKey, "-legacy")...)

	return vwc
}

// ReconcileWebhooks manages the creation and update of MutatingWebhookConfiguration and ValidatingWebhookConfiguration resources.
// It takes the owner object (e.g., DSCInitialization instance) to set owner references for garbage collection.
//
// Parameters:
//   - ctx: The context for the API call
//   - c: The client to use for the API call
//   - scheme: The scheme to use for the API call
//   - owner: The owner object to set owner references for garbage collection
//
// Returns:
//   - error: Any error encountered while reconciling the webhooks
func ReconcileWebhooks(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner metav1.Object) error {
	log := logf.FromContext(ctx).WithName(WebhookManagerName)

	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil || operatorNs == "" {
		// falling back for testing and envtest cases
		operatorNs = os.Getenv("OPERATOR_NAMESPACE")
	}

	// Define desired MutatingWebhookConfiguration and ValidatingWebhookConfiguration
	validatingWebhookConfig := DesiredValidatingWebhookConfiguration(operatorNs)

	// Set owner references to the owner object (e.g., DSCInitialization instance)
	// Kubernetes will delete these webhooks when the owner object is deleted.
	if err := controllerutil.SetOwnerReference(owner, validatingWebhookConfig, scheme); err != nil {
		log.Error(err, "Failed to set owner reference for ValidatingWebhookConfiguration")
		return err
	}

	applyOpts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(WebhookManagerName),
	}

	// Create the ValidatingWebhookConfiguration
	// Important: For SSA, you should pass a desired object without ResourceVersion or ManagedFields
	validatingWebhookConfig.SetResourceVersion("")
	validatingWebhookConfig.SetManagedFields(nil)
	if err := c.Patch(ctx, validatingWebhookConfig, client.Apply, applyOpts...); err != nil {
		log.Error(err, "Failed to apply ValidatingWebhookConfiguration via SSA")
		return err
	}

	log.Info("Webhook configurations reconciled successfully.")
	return nil
}
