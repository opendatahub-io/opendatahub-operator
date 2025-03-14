// +groupName=datasciencecluster.opendatahub.io
package components

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// Component struct defines the basis for each OpenDataHub component configuration.
// +kubebuilder:object:generate=true
type Component struct {
	common.ManagementSpec `json:",inline"`
	common.DevFlagsSpec   `json:",inline"`
}
