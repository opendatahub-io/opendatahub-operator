package clusterhealth

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KnownComponents maps component name (user-facing) to CR Kind.
var KnownComponents = map[string]string{
	"dashboard": "Dashboard", "workbenches": "Workbenches",
	"kserve": "Kserve", "ray": "Ray", "kueue": "Kueue",
	"modelregistry": "ModelRegistry", "trustyai": "TrustyAI",
	"datasciencepipelines": "DataSciencePipelines",
	"trainingoperator": "TrainingOperator", "feastoperator": "FeastOperator",
	"llamastackoperator": "LlamaStackOperator", "modelmeshserving": "ModelMeshServing",
	"mlflowoperator": "MLflowOperator", "sparkoperator": "SparkOperator",
	"modelcontroller": "ModelController", "modelsasservice": "ModelsAsService",
	"trainer": "Trainer",
}

// ComponentStatusResult holds the health details for a single ODH component.
type ComponentStatusResult struct {
	Component   string             `json:"component"`
	CRFound     bool               `json:"crFound"`
	Conditions  []ConditionSummary `json:"conditions"`
	Deployments []DeploymentInfo   `json:"deployments"`
	Pods        []PodInfo          `json:"pods"`
	Errors      []string           `json:"errors,omitempty"`
}

// GetComponentStatus fetches the CR conditions, deployments, and pods for a named component.
func GetComponentStatus(ctx context.Context, c client.Client, name, appsNS string) (*ComponentStatusResult, error) {
	kind, ok := KnownComponents[name]
	if !ok {
		names := make([]string, 0, len(KnownComponents))
		for n := range KnownComponents {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("unknown component %q; valid: %s", name, strings.Join(names, ", "))
	}

	result := &ComponentStatusResult{Component: name}

	// Component CR (singleton discovery).
	crList := &unstructured.UnstructuredList{}
	crList.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "components.platform.opendatahub.io", Version: "v1alpha1", Kind: kind + "List",
	})
	if err := c.List(ctx, crList); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("CR lookup: %v", err))
	} else if len(crList.Items) > 0 {
		result.CRFound = true
		var parseErr error
		result.Conditions, parseErr = parseConditionsFromUnstructured(crList.Items[0].Object)
		if parseErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("condition parse: %v", parseErr))
		}
	}

	// Labeled deployments and pods.
	label := client.MatchingLabels{"app.opendatahub.io/" + name: "true"}
	ns := client.InNamespace(appsNS)

	var deps appsv1.DeploymentList
	if err := c.List(ctx, &deps, ns, label); err != nil {
		return nil, fmt.Errorf("listing deployments for component %q: %w", name, err)
	}
	for i := range deps.Items {
		result.Deployments = append(result.Deployments, deploymentToInfo(&deps.Items[i]))
	}

	var pods corev1.PodList
	if err := c.List(ctx, &pods, ns, label); err != nil {
		return nil, fmt.Errorf("listing pods for component %q: %w", name, err)
	}
	for i := range pods.Items {
		result.Pods = append(result.Pods, podToInfo(&pods.Items[i]))
	}

	return result, nil
}
