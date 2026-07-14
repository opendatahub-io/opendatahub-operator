package kueue

import (
	"context"
	"fmt"
	"maps"
	"slices"

	operatorv1 "github.com/openshift/api/operator/v1"
	"golang.org/x/mod/semver"
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
		"pod":                                      "Pod",
		"deployment":                               "Deployment",
		"statefulset":                              "StatefulSet",
		"batch/job":                                "BatchJob",
		"ray.io/rayjob":                            "RayJob",
		"ray.io/raycluster":                        "RayCluster",
		"jobset.x-k8s.io/jobset":                   "JobSet",
		"kubeflow.org/mpijob":                      "MPIJob",
		"kubeflow.org/paddlejob":                   "PaddleJob",
		"kubeflow.org/pytorchjob":                  "PyTorchJob",
		"kubeflow.org/tfjob":                       "TFJob",
		"kubeflow.org/xgboostjob":                  "XGBoostJob",
		"trainer.kubeflow.org/trainjob":            "TrainJob",
		"leaderworkerset.x-k8s.io/leaderworkerset": "LeaderWorkerSet",
		"sparkoperator.k8s.io/sparkapplication":    "SparkApplication",
	}

	// frameworkMinVersion maps framework names to the minimum Kueue version
	// that supports them. Frameworks not listed here are assumed to be
	// supported by all versions.
	frameworkMinVersion = map[string]string{
		"TrainJob":         "v1.2.0",
		"SparkApplication": "v1.4.0",
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

	kueueInfo, err := cluster.OperatorExists(ctx, rr.Client, kueueOperator)
	if err != nil {
		return nil, fmt.Errorf("failed to check if %s exists: %w", kueueOperator, err)
	}

	if kueueInfo == nil {
		return nil, ErrKueueOperatorNotInstalled
	}
	//
	// Conversions
	//

	integrations, err := convertIntegrations(managerConfig, kueueInfo.Version)
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

	config := map[string]any{}

	obj := map[string]any{
		"spec": map[string]any{
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

// FrameworkMinVersion returns a copy of the minimum Kueue version requirements
// for version-gated frameworks. Used by e2e tests to build version-aware assertions.
func FrameworkMinVersion() map[string]string {
	result := make(map[string]string, len(frameworkMinVersion))
	maps.Copy(result, frameworkMinVersion)
	return result
}

// isFrameworkSupported checks whether a framework is supported by the given
// Kueue version. Frameworks listed in frameworkMinVersion require at least that
// version; all others are assumed supported.
func isFrameworkSupported(framework, kueueVersion string) bool {
	minVersion, ok := frameworkMinVersion[framework]
	if !ok {
		return true
	}
	return semver.Compare(kueueVersion, minVersion) >= 0
}

// convertIntegrations converts the integrations section from ConfigMap to Kueue operator format.
func convertIntegrations(config map[string]any, kueueVersion string) (map[string]any, error) {
	integrations := map[string]any{}

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

	// Add version-gated default frameworks.
	for framework := range frameworkMinVersion {
		if isFrameworkSupported(framework, kueueVersion) {
			frameworkSet.Insert(framework)
		}
	}

	// Convert ConfigMap framework entries, filtering out any that are
	// unsupported by the installed Kueue version.
	for _, framework := range frameworks {
		if converted, ok := frameworkMapping[framework]; ok {
			if isFrameworkSupported(converted, kueueVersion) {
				frameworkSet.Insert(converted)
			}
		}
	}

	convertedFrameworks := frameworkSet.UnsortedList()
	if len(convertedFrameworks) > 0 {
		slices.Sort(convertedFrameworks)
		interfaceSlice := make([]any, len(convertedFrameworks))
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
			if isFrameworkSupported(converted, kueueVersion) {
				externalFrameworksSet.Insert(converted)
			}
		}
	}

	convertedExternalFrameworks := externalFrameworksSet.UnsortedList()
	if len(convertedExternalFrameworks) > 0 {
		slices.Sort(convertedExternalFrameworks)
		interfaceSlice := make([]any, len(convertedExternalFrameworks))
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
		interfaceSlice := make([]any, len(labelKeys))
		for i, v := range labelKeys {
			interfaceSlice[i] = v
		}
		integrations["labelKeys"] = interfaceSlice
	}

	return integrations, nil
}

// convertWorkloadManagement converts workload management configuration.
func convertWorkloadManagement(config map[string]any) (map[string]any, error) {
	manageJobsWithoutQueueName, found, err := unstructured.NestedBool(config, "manageJobsWithoutQueueName")
	if err != nil {
		return nil, fmt.Errorf("failed to extract manageJobsWithoutQueueName: %w", err)
	}

	if !found {
		return nil, nil
	}

	workloadMgmt := map[string]any{
		"labelPolicy": "QueueName",
	}

	if manageJobsWithoutQueueName {
		workloadMgmt["labelPolicy"] = "None"
	}

	return workloadMgmt, nil
}

func convertGangScheduling(config map[string]any) (map[string]any, error) {
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

	gangScheduling := map[string]any{
		"policy": "None",
	}

	gangScheduling["policy"] = "ByWorkload"

	byWorkload := map[string]any{
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

func convertPreemption(config map[string]any) (map[string]any, error) {
	fairSharing, found, err := unstructured.NestedMap(config, "fairSharing")
	if err != nil {
		return nil, fmt.Errorf("failed to extract fairSharing: %w", err)
	}
	if !found {
		return nil, nil
	}

	preemption := map[string]any{
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
