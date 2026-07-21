package modules

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// MirrorSubmoduleConditions exposes mirrorSubmoduleConditions for testing.
func MirrorSubmoduleConditions(
	rr *types.ReconciliationRequest,
	platformCtx *PlatformContext,
	moduleStatus *ModuleStatus,
	submodules []SubmoduleCondition,
	notReadyModules *[]string,
) {
	mirrorSubmoduleConditions(rr, platformCtx, moduleStatus, submodules, notReadyModules)
}

// SubmoduleConditionsFor exposes submoduleConditionsFor for testing.
func SubmoduleConditionsFor(h ModuleHandler) []SubmoduleCondition {
	return submoduleConditionsFor(h)
}

// WriteSubmoduleComponentStatus exposes writeSubmoduleComponentStatus for testing.
func WriteSubmoduleComponentStatus(platformCtx *PlatformContext, sm SubmoduleCondition, enabled bool) {
	writeSubmoduleComponentStatus(platformCtx, sm, enabled)
}

// SetSubmodulesFallback exposes setSubmodulesFallback for testing.
func SetSubmodulesFallback(
	rr *types.ReconciliationRequest,
	platformCtx *PlatformContext,
	submodules []SubmoduleCondition,
	parentDisabled bool,
	reason string,
	message string,
) {
	setSubmodulesFallback(rr, platformCtx, submodules, parentDisabled, reason, message)
}
