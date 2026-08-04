package trainingoperator

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
)

const (
	ComponentName = componentApi.TrainingOperatorComponentName

	ReadyConditionType = componentApi.TrainingOperatorKind + status.ReadySuffix
)
