package mcplifecycleoperator

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
)

const (
	ComponentName = componentApi.MCPLifecycleOperatorComponentName

	ReadyConditionType = componentApi.MCPLifecycleOperatorKind + status.ReadySuffix

	LegacyComponentName = ""
)
