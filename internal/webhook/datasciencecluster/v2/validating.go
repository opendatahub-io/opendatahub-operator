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

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/validate-datasciencecluster-v2,matchPolicy=Exact,mutating=false,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=create;update,versions=v2,name=datasciencecluster-v2-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for DataScienceCluster v2 validation webhooks.
// It enforces singleton creation rules, validates Kueue managementState, and always allows deletion.
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
	hookServer.Register("/validate-datasciencecluster-v2", &webhook.Admission{
		Handler:        v,
		LogConstructor: webhookutils.NewWebhookLogConstructor(v.Name),
	})
	return nil
}

// Handle processes admission requests for create and update operations on DataScienceCluster v2 resources.
// It enforces singleton and managementState rules, allowing other operations by default.
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

	if req.Kind.Kind != gvk.DataScienceCluster.Kind || req.Kind.Group != gvk.DataScienceCluster.Group || req.Kind.Version != gvk.DataScienceCluster.Version {
		err := fmt.Errorf("unexpected gvk: %v; expecting: %v", req.Kind, gvk.DataScienceCluster)
		logf.FromContext(ctx).Error(err, "got wrong group/version/kind")
		return admission.Errored(http.StatusBadRequest, err)
	}

	allowMessage := fmt.Sprintf("Operation %s on %s v2 allowed", req.Operation, req.Kind.Kind)

	switch req.Operation {
	case admissionv1.Create:
		return validate(ctx, []validationCheck{v.denyKueueManagedState, denyMultipleDsc}, allowMessage, v.Client, &req)
	case admissionv1.Update:
		return validate(ctx, []validationCheck{v.denyKueueManagedState}, allowMessage, v.Client, &req)
	default:
		return admission.Allowed(allowMessage)
	}
}

type validationCheck func(context.Context, client.Reader, *admission.Request) admission.Response

func validate(ctx context.Context, checks []validationCheck, allowedMessage string, cli client.Reader, request *admission.Request) admission.Response {
	for _, check := range checks {
		resp := check(ctx, cli, request)
		if !resp.Allowed {
			return resp
		}
	}

	return admission.Allowed(allowedMessage)
}

func denyMultipleDsc(ctx context.Context, cli client.Reader, req *admission.Request) admission.Response {
	return webhookutils.ValidateSingletonCreation(ctx, cli, req, gvk.DataScienceCluster)
}

func (v *Validator) denyKueueManagedState(ctx context.Context, _ client.Reader, req *admission.Request) admission.Response {
	dsc := &dscv2.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.Object, dsc); err != nil {
		logf.FromContext(ctx).Error(err, "Error converting request object to "+gvk.DataScienceCluster.String())
		return admission.Errored(http.StatusBadRequest, err)
	}
	if dsc.Spec.Components.Kueue.ManagementState == operatorv1.Managed {
		return admission.Denied("Managed is no longer supported as a managementState")
	}

	return admission.Allowed("")
}
