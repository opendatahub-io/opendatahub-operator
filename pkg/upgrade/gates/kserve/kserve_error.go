package kserve

import (
	"fmt"
	"strings"
)

type UpgradeBlockedError struct {
	ServerlessInferenceServices     int
	ModelMeshInferenceServices      int
	MultiModelServingRuntimes       int
	RemovedRuntimeInferenceServices int
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 4)
	if e.ServerlessInferenceServices > 0 {
		parts = append(parts, fmt.Sprintf("%d Serverless InferenceServices", e.ServerlessInferenceServices))
	}
	if e.ModelMeshInferenceServices > 0 {
		parts = append(parts, fmt.Sprintf("%d ModelMesh InferenceServices", e.ModelMeshInferenceServices))
	}
	if e.MultiModelServingRuntimes > 0 {
		parts = append(parts, fmt.Sprintf("%d multi-model ServingRuntimes", e.MultiModelServingRuntimes))
	}
	if e.RemovedRuntimeInferenceServices > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d InferenceServices using removed runtimes",
			e.RemovedRuntimeInferenceServices,
		))
	}

	return "kserve blocking workloads found: " + strings.Join(parts, ", ")
}
