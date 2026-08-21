package kserve

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	deploymentModeAnnotation = "serving.kserve.io/deploymentMode"
	deploymentModeServerless = "Serverless"
	deploymentModeModelMesh  = "ModelMesh"
	kuadrantNamespace        = "kuadrant-system"
	authorinoName            = "authorino"
	readyConditionType       = "Ready"
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

	authorinoTLSNotReady, err := collectAuthorinoTLSReadinessBlock(ctx, reader)
	if err != nil {
		return err
	}

	blockingErr := &UpgradeBlockedError{
		ServerlessInferenceServices:     isvcCounts.serverless,
		ModelMeshInferenceServices:      isvcCounts.modelMesh,
		MultiModelServingRuntimes:       multiModelSRs,
		RemovedRuntimeInferenceServices: isvcCounts.removedRuntimes,
		AuthorinoTLSNotReady:            authorinoTLSNotReady,
	}
	if blockingErr.ServerlessInferenceServices == 0 &&
		blockingErr.ModelMeshInferenceServices == 0 &&
		blockingErr.MultiModelServingRuntimes == 0 &&
		blockingErr.RemovedRuntimeInferenceServices == 0 &&
		blockingErr.AuthorinoTLSNotReady == 0 {
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

func collectAuthorinoTLSReadinessBlock(ctx context.Context, reader client.Reader) (int, error) {
	llmInferenceServicesPresent, err := hasLLMInferenceServices(ctx, reader)
	if err != nil {
		return 0, err
	}
	if !llmInferenceServicesPresent {
		return 0, nil
	}

	ready, err := authorinoTLSReady(ctx, reader)
	if err != nil {
		return 0, err
	}
	if ready {
		return 0, nil
	}

	return 1, nil
}

func hasLLMInferenceServices(ctx context.Context, reader client.Reader) (bool, error) {
	for _, kind := range []struct {
		gvk  schema.GroupVersionKind
		name string
	}{
		{gvk: gvk.LLMInferenceServiceV1Alpha2, name: "LLMInferenceServices v1alpha2"},
		{gvk: gvk.LLMInferenceServiceV1Alpha1, name: "LLMInferenceServices v1alpha1"},
	} {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(kind.gvk)

		err := reader.List(ctx, list)
		switch {
		case meta.IsNoMatchError(err):
			continue
		case err != nil:
			return false, fmt.Errorf("listing %s: %w", kind.name, err)
		case len(list.Items) > 0:
			return true, nil
		}
	}

	return false, nil
}

func authorinoTLSReady(ctx context.Context, reader client.Reader) (bool, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.Authorinov1beta1)

	err := reader.Get(ctx, client.ObjectKey{Name: authorinoName, Namespace: kuadrantNamespace}, obj)
	switch {
	case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("getting Authorino %s/%s: %w", kuadrantNamespace, authorinoName, err)
	}

	tlsEnabled, found, err := unstructured.NestedBool(obj.Object, "spec", "listener", "tls", "enabled")
	if err != nil {
		return false, fmt.Errorf("reading Authorino TLS enabled: %w", err)
	}
	if !found || !tlsEnabled {
		return false, nil
	}

	certSecretName, found, err := unstructured.NestedString(obj.Object, "spec", "listener", "tls", "certSecretRef", "name")
	if err != nil {
		return false, fmt.Errorf("reading Authorino TLS certSecretRef.name: %w", err)
	}
	if !found || certSecretName == "" {
		return false, nil
	}

	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return false, fmt.Errorf("reading Authorino status.conditions: %w", err)
	}
	if !found {
		return false, nil
	}

	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]any)
		if !ok {
			continue
		}

		conditionType, _ := conditionMap["type"].(string)
		conditionStatus, _ := conditionMap["status"].(string)
		if conditionType == readyConditionType {
			return conditionStatus == "True", nil
		}
	}

	return false, nil
}
