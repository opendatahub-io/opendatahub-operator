package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	OGXComponentName = "ogx"
	OGXInstanceName  = "default-" + OGXComponentName
	OGXKind          = "OGX"
)

type OGXCommonSpec struct{}

type OGXCommonStatus struct{}

type DSCOGX struct {
	common.ManagementSpec `json:",inline"`
	OGXCommonSpec         `json:",inline"`
}

type DSCOGXStatus struct {
	common.ManagementSpec `json:",inline"`
	*OGXCommonStatus      `json:",inline"`
}
