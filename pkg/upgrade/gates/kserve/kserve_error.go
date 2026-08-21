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
	AuthorinoTLSNotReady            int
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 5)
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
	if e.AuthorinoTLSNotReady > 0 {
		parts = append(parts, "Authorino TLS readiness blocking llm-d workloads")
	}

	return "kserve blocking workloads found: " + strings.Join(parts, ", ")
}
