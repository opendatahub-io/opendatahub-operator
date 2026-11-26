//go:build !nowebhook

package v2

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	dscwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/validate-datasciencecluster-v2,matchPolicy=Exact,mutating=false,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=create;update,versions=v2,name=datasciencecluster-v2-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Validator implements webhook.AdmissionHandler for DataScienceCluster v2 validation webhooks.
// It enforces singleton and component validation rules, and always allows deletion.
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
// It enforces singleton, managementState and namespace-transition rules, allowing other operations by default.
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

	if req.Kind.Kind != gvk.DataScienceClusterV2.Kind || req.Kind.Group != gvk.DataScienceClusterV2.Group || req.Kind.Version != gvk.DataScienceClusterV2.Version {
		err := fmt.Errorf("unexpected gvk: %v; expecting: %v", req.Kind, gvk.DataScienceClusterV2)
		logf.FromContext(ctx).Error(err, "got wrong group/version/kind")
		return admission.Errored(http.StatusBadRequest, err)
	}

	allowMessage := fmt.Sprintf("Operation %s on %s v2 allowed", req.Operation, req.Kind.Kind)

	switch req.Operation {
	case admissionv1.Create:
		return validate(ctx, []validationCheck{
			denyMultipleDsc,
			v.denyKueueManagedState,
			v.denyRetiredOperatorManagedState,
			v.denyUnsupportedWVA,
		}, allowMessage, v.Client, &req)
	case admissionv1.Update:
		return validate(ctx, []validationCheck{
			v.denyKueueManagedState,
			v.denyRetiredOperatorManagedState,
			v.denyUnsupportedWVA,
			v.denyManagedDCHNamespaceChange,
		}, allowMessage, v.Client, &req)
	default:
		return admission.Allowed(allowMessage)
	}
}

type validationCheck func(context.Context, client.Reader, *admission.Request) admission.Response

// denyManagedDCHNamespaceChange mirrors the v3 CEL transition rule for DCH
// settings carried in the v2 conversion annotation, outside the v3 schema.
func (v *Validator) denyManagedDCHNamespaceChange(_ context.Context, _ client.Reader, req *admission.Request) admission.Response {
	current := &dscv2.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.Object, current); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode new DSC for DCH validation: %w", err))
	}
	if _, found := current.Annotations[dscv2.DataConnectHubV3StateAnnotation]; !found {
		return admission.Allowed("")
	}
	// Use the existing conversion to retain annotation validation safeguards.
	converted := &dscApi.DataScienceCluster{}
	if err := current.ConvertTo(converted); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("convert new DSC for DCH validation: %w", err))
	}
	config := converted.Spec.Components.Data.DataConnectHub
	if config.ManagementState != operatorv1.Managed {
		return admission.Allowed("")
	}
	// v3 defaulting resolves an empty Managed namespace before CEL runs.
	if config.Namespace == "" {
		config.Namespace = componentApi.DefaultDataConnectHubNamespace
	}
	if len(req.OldObject.Raw) == 0 {
		return admission.Errored(http.StatusBadRequest, errors.New("old DSC object is required for DCH namespace validation"))
	}
	old := &dscv2.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.OldObject, old); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode old DSC for DCH validation: %w", err))
	}
	previous := &dscApi.DataScienceCluster{}
	if err := old.ConvertTo(previous); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("convert old DSC for DCH validation: %w", err))
	}
	oldConfig := previous.Spec.Components.Data.DataConnectHub
	if oldConfig.ManagementState == operatorv1.Managed && oldConfig.Namespace != "" && oldConfig.Namespace != config.Namespace {
		return admission.Denied("DCH namespace is immutable while Managed; set Removed before changing it")
	}
	return admission.Allowed("")
}

func validate(ctx context.Context, checks []validationCheck, allowedMessage string, cli client.Reader, request *admission.Request) admission.Response {
	var warnings []string
	for _, check := range checks {
		resp := check(ctx, cli, request)
		if !resp.Allowed {
			return resp
		}
		warnings = append(warnings, resp.Warnings...)
	}

	resp := admission.Allowed(allowedMessage)
	resp.Warnings = warnings
	return resp
}

func denyMultipleDsc(ctx context.Context, cli client.Reader, req *admission.Request) admission.Response {
	return webhookutils.ValidateSingletonCreation(ctx, cli, req, gvk.DataScienceClusterV2)
}

func (v *Validator) denyKueueManagedState(ctx context.Context, _ client.Reader, req *admission.Request) admission.Response {
	dsc := &dscv2.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.Object, dsc); err != nil {
		logf.FromContext(ctx).Error(err, "Error converting request object to "+gvk.DataScienceClusterV2.String())
		return admission.Errored(http.StatusBadRequest, err)
	}
	if dsc.Spec.Components.Kueue.ManagementState == operatorv1.Managed {
		return admission.Denied("Managed is no longer supported as a managementState")
	}

	return admission.Allowed("")
}

func (v *Validator) denyUnsupportedWVA(ctx context.Context, _ client.Reader, req *admission.Request) admission.Response {
	dsc := &dscv2.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.Object, dsc); err != nil {
		logf.FromContext(ctx).Error(err, "Error converting request object to "+gvk.DataScienceClusterV2.String())
		return admission.Errored(http.StatusBadRequest, err)
	}

	return dscwebhook.WVAUnsupportedResponse(dsc.Spec.Components.Kserve.WVA.ManagementState)
}

// denyRetiredOperatorManagedState prevents enabling retired components while
// allowing a component that was already Managed to remain Managed until users
// explicitly remove it.
func (v *Validator) denyRetiredOperatorManagedState(ctx context.Context, _ client.Reader, req *admission.Request) admission.Response {
	dsc := &dscv2.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.Object, dsc); err != nil {
		logf.FromContext(ctx).Error(err, "Error decoding new DataScienceCluster v2 object")
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode new DSC: %w", err))
	}

	retiredComponents := []struct {
		newManaged bool
		oldManaged bool
		message    string
	}{
		{
			newManaged: dsc.Spec.Components.TrainingOperator.ManagementState == operatorv1.Managed,
			message:    "TrainingOperator v1 is obsolete in RHOAI 3.6. Set managementState to Removed, then delete the TrainingOperator CR to clean up. Use Trainer v2 instead.",
		},
		{
			newManaged: dsc.Spec.Components.LlamaStackOperator.ManagementState == operatorv1.Managed,
			message:    "LlamaStackOperator has been replaced by OGX. Set managementState to Removed.",
		},
	}

	if req.Operation == admissionv1.Create {
		for _, component := range retiredComponents {
			if component.newManaged {
				return admission.Denied(component.message)
			}
		}
		return admission.Allowed("")
	}

	needsOldObject := false
	for _, component := range retiredComponents {
		needsOldObject = needsOldObject || component.newManaged
	}
	if !needsOldObject {
		return admission.Allowed("")
	}
	if len(req.OldObject.Raw) == 0 {
		return admission.Errored(http.StatusBadRequest, errors.New("old DSC object is required for update validation"))
	}
	old := &dscv2.DataScienceCluster{}
	if err := v.Decoder.DecodeRaw(req.OldObject, old); err != nil {
		logf.FromContext(ctx).Error(err, "Error decoding old DataScienceCluster v2 object")
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode old DSC: %w", err))
	}
	retiredComponents[0].oldManaged = old.Spec.Components.TrainingOperator.ManagementState == operatorv1.Managed
	retiredComponents[1].oldManaged = old.Spec.Components.LlamaStackOperator.ManagementState == operatorv1.Managed

	for _, component := range retiredComponents {
		if component.newManaged && !component.oldManaged {
			return admission.Denied(component.message)
		}
	}

	return admission.Allowed("")
}
