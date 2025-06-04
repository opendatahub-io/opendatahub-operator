//go:build !nowebhook

package dscinitialization

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/shared"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

//+kubebuilder:webhook:path=/validate-dscinitialization,mutating=false,failurePolicy=fail,sideEffects=None,groups=dscinitialization.opendatahub.io,resources=dscinitializations,verbs=create;delete,versions=v1,name=dscinitialization-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for DSCInitialization validation webhooks.
// It enforces singleton creation and deletion rules for DSCInitialization resources.
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
	hookServer.Register("/validate-dscinitialization", &webhook.Admission{
		Handler:        v,
		LogConstructor: shared.NewLogConstructor(v.Name),
	})
	return nil
}

// Handle processes admission requests for create and delete operations on DSCInitialization resources.
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
		resp = shared.ValidateDupCreation(ctx, v.Client, &req, gvk.DSCInitialization.Kind)
	case admissionv1.Delete:
		resp = shared.DenyCountGtZero(ctx, v.Client, gvk.DataScienceCluster,
			"Cannot delete DSCInitialization object when DataScienceCluster object still exists")
	default:
		resp.Allowed = true
	}

	if !resp.Allowed {
		return resp
	}

	return admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
}
