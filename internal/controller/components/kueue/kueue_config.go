package kueue

import (
	"context"
	"fmt"
	"slices"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var (
	frameworkMapping = map[string]string{
		"pod":                     "Pod",
		"deployment":              "Deployment",
		"statefulset":             "StatefulSet",
		"batch/job":               "BatchJob",
		"ray.io/rayjob":           "RayJob",
		"ray.io/raycluster":       "RayCluster",
		"jobset.x-k8s.io/jobset":  "JobSet",
		"kubeflow.org/mpijob":     "MPIJob",
		"kubeflow.org/paddlejob":  "PaddleJob",
		"kubeflow.org/pytorchjob": "PyTorchJob",
		"kubeflow.org/tfjob":      "TFJob",
		"kubeflow.org/xgboostjob": "XGBoostJob",
		"leaderworkerset.x-k8s.io/leaderworkerset": "LeaderWorkerSet",
	}
)

func lookupKueueManagerConfig(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	cm := corev1.ConfigMap{}
	config := map[string]any{}

	// Fetch application namespace from DSCI.
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return nil, err
	}

	err = rr.Client.Get(
		ctx,
		client.ObjectKey{Name: KueueConfigMapName, Namespace: appNamespace},
		&cm,
	)

	switch {
	case k8serr.IsNotFound(err):
		return config, nil
	case err != nil:
		return nil, err
	}

	content, ok := cm.Data[KueueConfigMapEntry]
	if !ok {
		return config, nil
	}

	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return nil, err
	}

	return config, nil
}

func createKueueCR(ctx context.Context, rr *odhtypes.ReconciliationRequest) (*unstructured.Unstructured, error) {
	managerConfig, err := lookupKueueManagerConfig(ctx, rr)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup kueue manager config: %w", err)
	}

	//
	// Conversions
	//

	integrations, err := convertIntegrations(managerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert integrations: %w", err)
	}

	workloadMgmt, err := convertWorkloadManagement(managerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert workload management: %w", err)
	}

	gangScheduling, err := convertGangScheduling(managerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert gang scheduling: %w", err)
	}

	preemption, err := convertPreemption(managerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert preemption: %w", err)
	}

	//
	// Spec
	//

	config := map[string]interface{}{}

	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"managementState": string(operatorv1.Managed),
			"config":          config,
		},
	}

	if len(integrations) != 0 {
		config["integrations"] = integrations
	}
	if len(workloadMgmt) != 0 {
		config["workloadManagement"] = workloadMgmt
	}
	if len(gangScheduling) != 0 {
		config["gangScheduling"] = gangScheduling
	}
	if len(preemption) != 0 {
		config["preemption"] = preemption
	}

	// use resources.ToUnstructured so that it the content gets cleaned up
	// in case there are some misconfiguration i.e. int vs int64
	u, err := resources.ToUnstructured(&obj)
	if err != nil {
		return nil, fmt.Errorf("failed to build unstructured content: %w", err)
	}

	u.SetGroupVersionKind(gvk.KueueConfigV1)
	u.SetName(KueueCRName)

	// Set annotations to indicate this is not managed by ODH operator
	u.SetAnnotations(map[string]string{
		annotations.ManagedByODHOperator: "false",
	})

	return u, nil
}

// convertIntegrations converts the integrations section from ConfigMap to Kueue operator format.
func convertIntegrations(config map[string]interface{}) (map[string]interface{}, error) {
	integrations := map[string]interface{}{}

	//
	// integrations/frameworks
	//

	frameworks, _, err := unstructured.NestedStringSlice(config, "integrations", "frameworks")
	if err != nil {
		return nil, fmt.Errorf("failed to extract frameworks: %w", err)
	}

	frameworkSet := sets.New[string](
		"RayJob",
		"RayCluster",
		"PyTorchJob",
		"Pod",
		"Deployment",
		"StatefulSet",
	)

	for _, framework := range frameworks {
		if converted, ok := frameworkMapping[framework]; ok {
			frameworkSet.Insert(converted)
		}
	}

	convertedFrameworks := frameworkSet.UnsortedList()
	if len(convertedFrameworks) > 0 {
		slices.Sort(convertedFrameworks)
		interfaceSlice := make([]interface{}, len(convertedFrameworks))
		for i, v := range convertedFrameworks {
			interfaceSlice[i] = v
		}
		integrations["frameworks"] = interfaceSlice
	}

	//
	// integrations/externalFrameworks
	//

	externalFrameworksSet := sets.New[string]()

	externalFrameworks, _, err := unstructured.NestedStringSlice(config, "integrations", "externalFrameworks")
	if err != nil {
		return nil, fmt.Errorf("failed to extract external frameworks: %w", err)
	}

	for _, framework := range externalFrameworks {
		if converted, ok := frameworkMapping[framework]; ok {
			externalFrameworksSet.Insert(converted)
		}
	}

	convertedExternalFrameworks := externalFrameworksSet.UnsortedList()
	if len(convertedExternalFrameworks) > 0 {
		slices.Sort(convertedExternalFrameworks)
		interfaceSlice := make([]interface{}, len(convertedExternalFrameworks))
		for i, v := range convertedExternalFrameworks {
			interfaceSlice[i] = v
		}
		integrations["externalFrameworks"] = interfaceSlice
	}

	//
	// integrations/labelKeys
	//

	labelKeys, _, err := unstructured.NestedStringSlice(config, "integrations", "labelKeys")
	if err != nil {
		return nil, fmt.Errorf("failed to extract label keys: %w", err)
	}

	if len(labelKeys) > 0 {
		slices.Sort(labelKeys)
		interfaceSlice := make([]interface{}, len(labelKeys))
		for i, v := range labelKeys {
			interfaceSlice[i] = v
		}
		integrations["labelKeys"] = interfaceSlice
	}

	return integrations, nil
}

// convertWorkloadManagement converts workload management configuration.
func convertWorkloadManagement(config map[string]interface{}) (map[string]interface{}, error) {
	manageJobsWithoutQueueName, found, err := unstructured.NestedBool(config, "manageJobsWithoutQueueName")
	if err != nil {
		return nil, fmt.Errorf("failed to extract manageJobsWithoutQueueName: %w", err)
	}

	if !found {
		return nil, nil
	}

	workloadMgmt := map[string]interface{}{
		"labelPolicy": "QueueName",
	}

	if manageJobsWithoutQueueName {
		workloadMgmt["labelPolicy"] = "None"
	}

	return workloadMgmt, nil
}

func convertGangScheduling(config map[string]interface{}) (map[string]interface{}, error) {
	waitForPodsReady, found, err := unstructured.NestedMap(config, "waitForPodsReady")
	if err != nil {
		return nil, fmt.Errorf("failed to extract waitForPodsReady: %w", err)
	}

	if !found {
		return nil, nil
	}

	enabled, _, err := unstructured.NestedBool(waitForPodsReady, "enable")
	if err != nil {
		return nil, fmt.Errorf("failed to extract waitForPodsReady.enable: %w", err)
	}

	if !enabled {
		return nil, nil
	}

	gangScheduling := map[string]interface{}{
		"policy": "None",
	}

	gangScheduling["policy"] = "ByWorkload"

	byWorkload := map[string]interface{}{
		"admission": "Parallel", // Default to Parallel
	}

	blockAdmission, _, err := unstructured.NestedBool(waitForPodsReady, "blockAdmission")
	if err != nil {
		return nil, fmt.Errorf("failed to extract waitForPodsReady.blockAdmission: %w", err)
	}

	if blockAdmission {
		byWorkload["admission"] = "Sequential"
	}

	timeout, _, err := unstructured.NestedString(waitForPodsReady, "timeout")
	if err != nil {
		return nil, fmt.Errorf("failed to extract timeout: %w", err)
	}

	if timeout != "" {
		byWorkload["timeout"] = timeout
	}

	gangScheduling["byWorkload"] = byWorkload

	return gangScheduling, nil
}

func convertPreemption(config map[string]interface{}) (map[string]interface{}, error) {
	fairSharing, found, err := unstructured.NestedMap(config, "fairSharing")
	if err != nil {
		return nil, fmt.Errorf("failed to extract fairSharing: %w", err)
	}
	if !found {
		return nil, nil
	}

	preemption := map[string]interface{}{
		"preemptionPolicy": "Classical",
	}

	if found {
		// Check if fair sharing is enabled
		enabled, _, err := unstructured.NestedBool(fairSharing, "enable")
		if err != nil {
			return nil, fmt.Errorf("failed to extract fairSharing.enable: %w", err)
		}

		if enabled {
			preemption["preemptionPolicy"] = "FairSharing"
			// Include the fairSharing configuration from the original config
			preemption["fairSharing"] = fairSharing
		}
	}

	return preemption, nil
}
