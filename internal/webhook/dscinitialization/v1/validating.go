//go:build !nowebhook

package v1

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/validate-dscinitialization-v1,matchPolicy=Exact,mutating=false,failurePolicy=fail,sideEffects=None,groups=dscinitialization.opendatahub.io,resources=dscinitializations,verbs=create;delete,versions=v1,name=dscinitialization-v1-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for DSCInitialization v1 validation webhooks.
// It enforces singleton creation and deletion rules for DSCInitialization resources.
type Validator struct {
	Client client.Reader
	Name   string
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
	hookServer.Register("/validate-dscinitialization-v1", &webhook.Admission{
		Handler:        v,
		LogConstructor: webhookutils.NewWebhookLogConstructor(v.Name),
	})
	return nil
}

// Handle processes admission requests for create and delete operations on DSCInitialization v1 resources.
// It enforces singleton and deletion rules, allowing other operations by default.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - req: The admission.Request containing the operation and object details.
//
// Returns:
//   - admission.Response: The result of the admission check, indicating whether the operation is allowed or denied.
func (v *Validator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)
	ctx = logf.IntoContext(ctx, log)

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create:
		resp = webhookutils.ValidateSingletonCreation(ctx, v.Client, &req, gvk.DSCInitialization)
	case admissionv1.Delete:
		resp = webhookutils.DenyCountGtZero(ctx, v.Client, gvk.DataScienceCluster,
			"Cannot delete DSCInitialization v1 object when DataScienceCluster object still exists")
	default:
		resp.Allowed = true
	}

	if !resp.Allowed {
		return resp
	}

	return admission.Allowed(fmt.Sprintf("Operation %s on %s v1 allowed", req.Operation, req.Kind.Kind))
}
