package kserve

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	deploymentModeAnnotation = "serving.kserve.io/deploymentMode"
	deploymentModeServerless = "Serverless"
	deploymentModeModelMesh  = "ModelMesh"
)

var removedRuntimes = map[string]bool{
	"ovms":                               true,
	"caikit-standalone-serving-template": true,
	"caikit-tgis-serving-template":       true,
}

type inferenceServiceCounts struct {
	serverless      int
	modelMesh       int
	removedRuntimes int
}

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	isvcCounts, err := collectInferenceServiceCounts(ctx, reader)
	if err != nil {
		return err
	}

	multiModelSRs, err := collectMultiModelServingRuntimeCount(ctx, reader)
	if err != nil {
		return err
	}

	blockingErr := &UpgradeBlockedError{
		ServerlessInferenceServices:     isvcCounts.serverless,
		ModelMeshInferenceServices:      isvcCounts.modelMesh,
		MultiModelServingRuntimes:       multiModelSRs,
		RemovedRuntimeInferenceServices: isvcCounts.removedRuntimes,
	}
	if blockingErr.ServerlessInferenceServices == 0 &&
		blockingErr.ModelMeshInferenceServices == 0 &&
		blockingErr.MultiModelServingRuntimes == 0 &&
		blockingErr.RemovedRuntimeInferenceServices == 0 {
		return nil
	}

	return blockingErr
}

func collectInferenceServiceCounts(
	ctx context.Context,
	reader client.Reader,
) (inferenceServiceCounts, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.InferenceServices)

	err := reader.List(ctx, list)
	switch {
	case meta.IsNoMatchError(err):
		return inferenceServiceCounts{}, nil
	case err != nil:
		return inferenceServiceCounts{}, fmt.Errorf("listing InferenceServices: %w", err)
	}

	counts := inferenceServiceCounts{}
	for i := range list.Items {
		if resources.HasAnnotation(&list.Items[i], deploymentModeAnnotation, deploymentModeServerless) {
			counts.serverless++
		}
		if resources.HasAnnotation(&list.Items[i], deploymentModeAnnotation, deploymentModeModelMesh) {
			counts.modelMesh++
		}

		runtimeName, found, err := unstructured.NestedString(
			list.Items[i].Object, "spec", "predictor", "model", "runtime",
		)
		if err != nil {
			return inferenceServiceCounts{}, fmt.Errorf("reading InferenceService %s/%s runtime: %w",
				list.Items[i].GetNamespace(), list.Items[i].GetName(), err)
		}
		if found && removedRuntimes[runtimeName] {
			counts.removedRuntimes++
		}
	}

	return counts, nil
}

func collectMultiModelServingRuntimeCount(
	ctx context.Context,
	reader client.Reader,
) (int, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.ServingRuntime)

	err := reader.List(ctx, list)
	switch {
	case meta.IsNoMatchError(err):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("listing ServingRuntimes: %w", err)
	}

	count := 0
	for i := range list.Items {
		multiModel, found, err := unstructured.NestedBool(list.Items[i].Object, "spec", "multiModel")
		if err != nil {
			return 0, fmt.Errorf("reading ServingRuntime %s/%s: %w",
				list.Items[i].GetNamespace(), list.Items[i].GetName(), err)
		}
		if found && multiModel {
			count++
		}
	}

	return count, nil
}
