//go:build !nowebhook

package v2

import (
	"context"
	"fmt"
	"net/http"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/validate-dscinitialization-v2,matchPolicy=Exact,mutating=false,failurePolicy=fail,sideEffects=None,groups=dscinitialization.opendatahub.io,resources=dscinitializations,verbs=create;update;delete,versions=v2,name=dscinitialization-v2-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for DSCInitialization v2 validation webhooks.
// It enforces singleton creation and deletion rules for DSCInitialization resources.
type Validator struct {
	Client  client.Reader
	Name    string
	Decoder admission.Decoder
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
	hookServer.Register("/validate-dscinitialization-v2", &webhook.Admission{
		Handler:        v,
		LogConstructor: webhookutils.NewWebhookLogConstructor(v.Name),
	})
	return nil
}

// Handle processes admission requests for create, update, and delete operations on DSCInitialization v2 resources.
// It enforces singleton creation, PEM validation for customCABundle, and deletion rules.
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

	allowMessage := fmt.Sprintf("Operation %s on %s v2 allowed", req.Operation, req.Kind.Kind)

	switch req.Operation {
	case admissionv1.Create:
		if resp := webhookutils.ValidateSingletonCreation(ctx, v.Client, &req, gvk.DSCInitialization); !resp.Allowed {
			return resp
		}
		return v.validateSpec(ctx, &req, allowMessage)
	case admissionv1.Update:
		return v.validateSpec(ctx, &req, allowMessage)
	case admissionv1.Delete:
		resp := webhookutils.DenyCountGtZero(ctx, v.Client, gvk.DataScienceCluster,
			"Cannot delete DSCInitialization v2 object when DataScienceCluster object still exists")
		if !resp.Allowed {
			return resp
		}
		return admission.Allowed(allowMessage)
	default:
		return admission.Allowed(allowMessage)
	}
}

func (v *Validator) validateSpec(ctx context.Context, req *admission.Request, allowMessage string) admission.Response {
	dsci := &dsciv2.DSCInitialization{}
	if err := v.Decoder.DecodeRaw(req.Object, dsci); err != nil {
		logf.FromContext(ctx).Error(err, "failed to decode DSCInitialization v2")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if dsci.Spec.TrustedCABundle != nil &&
		dsci.Spec.TrustedCABundle.ManagementState == operatorv1.Managed &&
		dsci.Spec.TrustedCABundle.CustomCABundle != "" {
		if err := cluster.ValidateCustomCABundle(dsci.Spec.TrustedCABundle.CustomCABundle); err != nil {
			return admission.Denied(fmt.Sprintf("invalid customCABundle: %v", err))
		}
	}

	return admission.Allowed(allowMessage)
}
