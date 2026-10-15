//go:build !nowebhook

package datasciencecluster

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// DeprecatedWVAWarning is returned as an admission warning on DSC create/update
// when spec.components.kserve.wva is set. Admission only allows Removed/empty;
// Managed is rejected by WVAUnsupportedResponse.
const DeprecatedWVAWarning = "spec.components.kserve.wva is deprecated and WVA is no longer supported; " +
	"set it to Removed or remove the field. The operator treats WVA as Removed."

// WVAUnsupportedResponse rejects attempts to enable WVA and warns when the
// compatibility field is present with a non-Managed value.
func WVAUnsupportedResponse(state operatorv1.ManagementState) admission.Response {
	if state == operatorv1.Managed {
		return admission.Denied("spec.components.kserve.wva is no longer supported; set managementState to Removed or remove the field")
	}

	resp := admission.Allowed("")
	if state != "" {
		resp.Warnings = []string{DeprecatedWVAWarning}
	}

	return resp
}
