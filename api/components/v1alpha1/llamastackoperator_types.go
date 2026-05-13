package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// Deprecated: LlamaStackOperator has been renamed to OGX. The standalone LlamaStackOperator
// and LlamaStackOperatorList CRD types are no longer registered. DSCLlamaStackOperator and
// DSCLlamaStackOperatorStatus are kept for backward compatibility with DSC v1 and the
// deprecated v2 field only.

const (
	// Deprecated: Use OGXComponentName instead.
	LlamaStackOperatorComponentName = "llamastackoperator"
)

type LlamaStackOperatorCommonSpec struct {
	// new component spec exposed to DSC api
}

// LlamaStackOperatorCommonStatus defines the shared observed state of LlamaStackOperator
type LlamaStackOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// LlamaStackOperatorStatus defines the observed state of LlamaStackOperator
type LlamaStackOperatorStatus struct {
	common.Status                  `json:",inline"`
	LlamaStackOperatorCommonStatus `json:",inline"`
}

// DSCLlamaStackOperatorStatus struct holds the status for the LlamaStackOperator component exposed in the DSC
type DSCLlamaStackOperatorStatus struct {
	common.ManagementSpec           `json:",inline"`
	*LlamaStackOperatorCommonStatus `json:",inline"`
}

// Standalone LlamaStackOperator and LlamaStackOperatorList CRDs are no longer registered.
// The component has been renamed to OGX. DSCLlamaStackOperator and DSCLlamaStackOperatorStatus
// are kept for backward compatibility with DSC v1 and the deprecated v2 field.

// DSCLlamaStackOperator contains all the configuration exposed in DSC instance for LlamaStackOperator component
type DSCLlamaStackOperator struct {
	common.ManagementSpec `json:",inline"`

	LlamaStackOperatorCommonSpec `json:",inline"`
}
