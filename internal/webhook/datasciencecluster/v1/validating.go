//go:build !nowebhook

package v1

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

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/validate-datasciencecluster-v1,matchPolicy=Exact,mutating=false,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=create;update,versions=v1,name=datasciencecluster-v1-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for DataScienceCluster v1 validation webhooks.
// It enforces singleton creation rules for DataScienceCluster resources and always allows their deletion.
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
	hookServer.Register("/validate-datasciencecluster-v1", &webhook.Admission{
		Handler:        v,
		LogConstructor: webhookutils.NewWebhookLogConstructor(v.Name),
	})
	return nil
}

// Handle processes admission requests for create operations on DataScienceCluster v1 resources.
// It enforces singleton rules, allowing other operations by default.
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

	allowMessage := fmt.Sprintf("Operation %s on %s v1 allowed", req.Operation, req.Kind.Kind)

	if req.Kind.Kind != gvk.DataScienceClusterV1.Kind || req.Kind.Group != gvk.DataScienceClusterV1.Group || req.Kind.Version != gvk.DataScienceClusterV1.Version {
		err := fmt.Errorf("unexpected gvk: %v; expecting: %v", req.Kind, gvk.DataScienceClusterV1)
		logf.FromContext(ctx).Error(err, "got wrong group/version/kind")
		return admission.Errored(http.StatusBadRequest, err)
	}

	switch req.Operation {
	case admissionv1.Create:
		return validate([]validationCheck{v.denyManagementstateManaged, denyMultipleDsc}, allowMessage, ctx, v.Client, &req)
	case admissionv1.Update:
		return validate([]validationCheck{v.denyManagementstateManaged}, allowMessage, ctx, v.Client, &req)
	default:
		return admission.Allowed(allowMessage) // initialize Allowed to be true in case Operation falls into "default" case
	}
}

type validationCheck func(context.Context, client.Reader, *admission.Request) admission.Response

func validate(checks []validationCheck, allowedMessage string, ctx context.Context, client client.Reader, request *admission.Request) admission.Response {
	for _, check := range checks {
		resp := check(ctx, client, request)
		if !resp.Allowed {
			return resp
		}
	}

	return admission.Allowed(allowedMessage)
}

func denyMultipleDsc(ctx context.Context, client client.Reader, req *admission.Request) admission.Response {
	return webhookutils.ValidateSingletonCreation(ctx, client, req, gvk.DataScienceCluster)
}

func (v *Validator) denyManagementstateManaged(ctx context.Context, client client.Reader, req *admission.Request) admission.Response {
	dcsV1 := &dscv1.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.Object, dcsV1); err != nil {
		logf.FromContext(ctx).Error(err, "Error converting request object to "+gvk.DataScienceClusterV1.String())
		return admission.Errored(http.StatusBadRequest, err)
	}
	if dcsV1.Spec.Components.Kueue.ManagementState == operatorv1.Managed {
		return admission.Denied("Managed is no longer supported as a managementState")
	}

	return admission.Allowed("")
}
