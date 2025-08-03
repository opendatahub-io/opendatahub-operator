//go:build !nowebhook

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/validate-auth,mutating=false,failurePolicy=fail,sideEffects=None,groups=services.platform.opendatahub.io,resources=auths,verbs=create;update,versions=v1alpha1,name=auth-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Constants for invalid group values that should be rejected.
const (
	SystemAuthenticatedGroup = "system:authenticated"
	EmptyGroup               = ""
)

// Validator implements webhook.AdmissionHandler for Auth validation webhooks.
// It validates that AdminGroups and AllowedGroups don't contain invalid values.
type Validator struct {
	Client  client.Reader
	Decoder admission.Decoder
	Name    string
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
	hookServer.Register("/validate-auth", &webhook.Admission{
		Handler:        v,
		LogConstructor: webhookutils.NewWebhookLogConstructor(v.Name),
	})
	return nil
}

// Handle processes admission requests for create and update operations on Auth resources.
// It validates that AdminGroups and AllowedGroups don't contain invalid values.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - req: The admission.Request containing the operation and object details.
//
// Returns:
//   - admission.Response: The result of the admission check, indicating whether the operation is allowed or denied.
func (v *Validator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Check if decoder is properly injected
	if v.Decoder == nil {
		log.Error(nil, "Decoder is nil - webhook not properly initialized")
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
	}

	// Validate that we're processing the correct Kind
	if req.Kind.Kind != gvk.Auth.Kind {
		err := fmt.Errorf("unexpected kind: %s", req.Kind.Kind)
		log.Error(err, "got wrong kind")
		return admission.Errored(http.StatusBadRequest, err)
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		resp = v.validateAuthGroups(ctx, req)
	default:
		resp.Allowed = true
	}

	if !resp.Allowed {
		return resp
	}

	return admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
}

// validateAuthGroups validates that AdminGroups and AllowedGroups don't contain invalid values.
func (v *Validator) validateAuthGroups(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	// Decode the Auth object
	auth := &serviceApi.Auth{}
	if err := v.Decoder.Decode(req, auth); err != nil {
		log.Error(err, "failed to decode Auth object")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Validate both AdminGroups and AllowedGroups
	if invalidAdminGroups := validateAdminGroups(auth.Spec.AdminGroups); len(invalidAdminGroups) > 0 {
		msg := fmt.Sprintf("Invalid groups found in AdminGroups: %s. Groups cannot be '%s' or empty string",
			strings.Join(invalidAdminGroups, ", "), SystemAuthenticatedGroup)
		log.V(1).Info("Rejecting Auth resource due to invalid AdminGroups", "invalidGroups", invalidAdminGroups)
		return admission.Denied(msg)
	}

	if invalidAllowedGroups := validateAllowedGroups(auth.Spec.AllowedGroups); len(invalidAllowedGroups) > 0 {
		msg := fmt.Sprintf("Invalid groups found in AllowedGroups: %s. Groups cannot be empty string",
			strings.Join(invalidAllowedGroups, ", "))
		log.V(1).Info("Rejecting Auth resource due to invalid AllowedGroups", "invalidGroups", invalidAllowedGroups)
		return admission.Denied(msg)
	}

	return admission.Allowed("")
}

// validateAdminGroups checks AdminGroups for invalid values.
// AdminGroups cannot contain 'system:authenticated' (security risk) or empty strings.
func validateAdminGroups(groups []string) []string {
	if len(groups) == 0 {
		return nil
	}

	var invalidGroups []string
	for _, group := range groups {
		if group == SystemAuthenticatedGroup || group == EmptyGroup {
			invalidGroups = append(invalidGroups, fmt.Sprintf("'%s'", group))
		}
	}
	return invalidGroups
}

// validateAllowedGroups checks AllowedGroups for invalid values.
// AllowedGroups cannot contain empty strings, but 'system:authenticated' is allowed for general access.
func validateAllowedGroups(groups []string) []string {
	if len(groups) == 0 {
		return nil
	}

	var invalidGroups []string
	for _, group := range groups {
		if group == EmptyGroup {
			invalidGroups = append(invalidGroups, fmt.Sprintf("'%s'", group))
		}
	}
	return invalidGroups
}
