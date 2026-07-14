//go:build !nowebhook

package datasciencecluster

import (
	operatorv1 "github.com/openshift/api/operator/v1"
)

// DeprecatedModelsAsServiceWarning is returned as an admission warning on DSC
// create/update when spec.components.kserve.modelsAsService is Managed.
// kubectl/oc prints it as: Warning: <message>.
const DeprecatedModelsAsServiceWarning = "spec.components.kserve.modelsAsService is deprecated; " +
	"use spec.components.aigateway.modelsAsAService instead. " +
	"Clear to Removed after migration (re-enabling is blocked). " +
	"The field remains respected at least through 3.6."

// ModelsAsServiceDeprecationWarnings returns admission warnings when the
// deprecated kserve.modelsAsService field is actively enabling MaaS.
func ModelsAsServiceDeprecationWarnings(state operatorv1.ManagementState) []string {
	if state == operatorv1.Managed {
		return []string{DeprecatedModelsAsServiceWarning}
	}
	return nil
}
