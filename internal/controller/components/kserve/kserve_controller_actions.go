package kserve

import (
	"context"
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	LLMInferenceServiceConfigWellKnownAnnotationKey   = "serving.kserve.io/well-known-config"
	LLMInferenceServiceConfigWellKnownAnnotationValue = "true"

	// ServiceMesh operator constants.
	serviceMeshOperatorSubscription = "servicemeshoperator"
	serviceMeshOperatorPrefix       = "servicemeshoperator"
	requiredServiceMeshMajorVersion = uint64(3)

	// ServiceMesh version check condition reasons.
	reasonServiceMeshNotInstalled = "ServiceMeshNotInstalled"
	reasonVersionCheckPassed      = "VersionCheckPassed"
	reasonAPIError                = "APIError"
	reasonOperatorNotRunning      = "OperatorNotRunning"
	reasonUnparseableVersion      = "UnparseableVersion"
	reasonIncompatibleVersion     = "IncompatibleVersion"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = []odhtypes.ManifestInfo{
		kserveManifestInfo(kserveManifestSourcePath),
		{
			Path:       odhdeploy.DefaultManifestPath,
			ContextDir: "connectionAPI",
		},
	}

	return nil
}

func removeOwnershipFromUnmanagedResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	for _, res := range rr.Resources {
		if shouldRemoveOwnerRefAndLabel(res) {
			if err := getAndRemoveOwnerReferences(ctx, rr.Client, res, isKserveOwnerRef); err != nil {
				return odherrors.NewStopErrorW(err)
			}
		}
	}

	return nil
}

func cleanUpTemplatedResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	logger := logf.FromContext(ctx)

	for _, res := range rr.Resources {
		if isForDependency("serverless")(&res) || isForDependency("servicemesh")(&res) {
			err := rr.Client.Delete(ctx, &res, client.PropagationPolicy(metav1.DeletePropagationForeground))
			if err != nil {
				if k8serr.IsNotFound(err) {
					continue
				}
				if meta.IsNoMatchError(err) { // when CRD is missing,
					continue
				}
				return odherrors.NewStopErrorW(err)
			}
			logger.Info("Deleted", "kind", res.GetKind(), "name", res.GetName(), "namespace", res.GetNamespace())
		}
	}

	if err := rr.RemoveResources(isForDependency("servicemesh")); err != nil {
		return odherrors.NewStopErrorW(err)
	}

	if err := rr.RemoveResources(isForDependency("serverless")); err != nil {
		return odherrors.NewStopErrorW(err)
	}

	return nil
}

func customizeKserveConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	kserveConfigMap := corev1.ConfigMap{}
	cmidx, err := getIndexedResource(rr.Resources, &kserveConfigMap, gvk.ConfigMap, kserveConfigMapName)
	if err != nil {
		return err
	}

	//nolint:staticcheck
	serviceClusterIPNone := true
	if k.Spec.RawDeploymentServiceConfig == componentApi.KserveRawHeaded {
		// As default is Headless, only set false here if Headed is explicitly set
		serviceClusterIPNone = false
	}

	if err := updateInferenceCM(&kserveConfigMap, serviceClusterIPNone); err != nil {
		return err
	}

	if err = replaceResourceAtIndex(rr.Resources, cmidx, &kserveConfigMap); err != nil {
		return err
	}
	kserveConfigHash, err := hashConfigMap(&kserveConfigMap)
	if err != nil {
		return err
	}

	kserveDeployment := appsv1.Deployment{}
	deployidx, err := getIndexedResource(rr.Resources, &kserveDeployment, gvk.Deployment, "kserve-controller-manager")
	if err != nil {
		return err
	}
	kserveDeployment.Spec.Template.Annotations[labels.ODHAppPrefix+"/KserveConfigHash"] = kserveConfigHash

	if err = replaceResourceAtIndex(rr.Resources, deployidx, &kserveDeployment); err != nil {
		return err
	}

	return nil
}

func versionedWellKnownLLMInferenceServiceConfigs(_ context.Context, version string, rr *odhtypes.ReconciliationRequest) error {
	const (
		envFormat = "%s-kserve-"
		envName   = "LLM_INFERENCE_SERVICE_CONFIG_PREFIX"
	)

	for i := range rr.Resources {
		if rr.Resources[i].GroupVersionKind().Group == gvk.LLMInferenceServiceConfigV1Alpha1.Group &&
			rr.Resources[i].GroupVersionKind().Kind == gvk.LLMInferenceServiceConfigV1Alpha1.Kind {
			if v, ok := rr.Resources[i].GetAnnotations()[LLMInferenceServiceConfigWellKnownAnnotationKey]; ok && v == LLMInferenceServiceConfigWellKnownAnnotationValue {
				rr.Resources[i].SetName(fmt.Sprintf("%s-%s", version, rr.Resources[i].GetName()))
			}
		}

		if rr.Resources[i].GroupVersionKind().Group == gvk.Deployment.Group &&
			rr.Resources[i].GroupVersionKind().Kind == gvk.Deployment.Kind {
			deployment := &appsv1.Deployment{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[i].Object, deployment); err != nil {
				return err
			}

			for j := range deployment.Spec.Template.Spec.Containers {
				container := &deployment.Spec.Template.Spec.Containers[j]
				envVarFound := false

				for k := range container.Env {
					if container.Env[k].Name == envName {
						container.Env[k].Value = fmt.Sprintf(envFormat, version)
						envVarFound = true
						break
					}
				}

				if !envVarFound {
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  envName,
						Value: fmt.Sprintf(envFormat, version),
					})
				}
			}

			u, err := resources.ToUnstructured(deployment)
			if err != nil {
				return err
			}
			rr.Resources[i] = *u
		}
	}
	return nil
}

// extractMajorVersion parses version string and returns major version number using semver.
// Handles standard semver formats as well as shortened versions like "v3.0", "3", etc.
// Also supports pre-release versions (e.g., "v3.0.0-rc1") and build metadata (e.g., "v3.0.0+build123").
func extractMajorVersion(version string) (uint64, error) {
	v, err := semver.ParseTolerant(version)
	if err != nil {
		return 0, fmt.Errorf("failed to parse version %s: %w", version, err)
	}
	return v.Major, nil
}

// setServiceMeshConditionAndStopError sets a ServiceMeshVersionRequirement condition on the Kserve instance
// and returns a StopError with the provided message. This helper reduces duplication in error handling paths.
func setServiceMeshConditionAndStopError(k *componentApi.Kserve, reason, conditionMsg, stopErrorMsg string, args ...interface{}) error {
	conditions.SetStatusCondition(k, common.Condition{
		Type:    ServiceMeshVersionRequirement,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: conditionMsg,
	})
	return odherrors.NewStopError(stopErrorMsg, args...)
}

// validateServiceMeshOperatorVersion retrieves the ServiceMesh operator information and validates
// that it is running version 3. Returns nil if validation passes, or an error if the operator
// is not running, version cannot be parsed, or version is incompatible.
func validateServiceMeshOperatorVersion(ctx context.Context, rr *odhtypes.ReconciliationRequest, k *componentApi.Kserve) error {
	logger := logf.FromContext(ctx)

	// Retrieve operator information
	operatorInfo, err := cluster.OperatorExists(ctx, rr.Client, serviceMeshOperatorPrefix)
	if err != nil {
		logger.Error(err, "Failed to retrieve ServiceMesh operator version information")
		return setServiceMeshConditionAndStopError(k,
			reasonAPIError,
			"Unable to retrieve ServiceMesh operator version due to API error",
			"ServiceMesh subscription found but unable to retrieve operator version due to API error. "+
				"Cannot proceed with KServe deployment without confirming ServiceMesh version compatibility. "+
				"Please check API access, RBAC permissions, and that the OperatorCondition CRD is available.",
		)
	}

	if operatorInfo == nil {
		return setServiceMeshConditionAndStopError(k,
			reasonOperatorNotRunning,
			"ServiceMesh subscription found but operator is not running",
			"OpenShift ServiceMesh subscription found but operator is not running. "+
				"Unable to verify version requirement. "+
				"Please ensure ServiceMesh v3.x operator is running or uninstall ServiceMesh before enabling KServe.",
		)
	}

	// Parse and validate version
	version := operatorInfo.Version
	majorVersion, err := extractMajorVersion(version)
	if err != nil {
		return setServiceMeshConditionAndStopError(k,
			reasonUnparseableVersion,
			fmt.Sprintf("ServiceMesh version %s cannot be parsed", version),
			"OpenShift ServiceMesh detected with unparseable version: %s. "+
				"Unable to verify version requirement. "+
				"Please ensure ServiceMesh v3.x is installed or uninstall ServiceMesh before enabling KServe.",
			version,
		)
	}

	// Only major version 3 is compatible
	if majorVersion != requiredServiceMeshMajorVersion {
		return setServiceMeshConditionAndStopError(k,
			reasonIncompatibleVersion,
			fmt.Sprintf("ServiceMesh version %s detected, requires v3.x", version),
			"OpenShift ServiceMesh version %s detected. KServe requires ServiceMesh version 3.x when ServiceMesh is installed. "+
				"Please either upgrade to ServiceMesh v3.x or uninstall ServiceMesh before enabling KServe.",
			version,
		)
	}

	// Version check passed - ServiceMesh v3 is installed
	conditions.SetStatusCondition(k, common.Condition{
		Type:    ServiceMeshVersionRequirement,
		Status:  metav1.ConditionTrue,
		Reason:  reasonVersionCheckPassed,
		Message: fmt.Sprintf("OpenShift ServiceMesh version %s meets requirement (v3.x required when installed)", version),
	})

	logger.Info("ServiceMesh version check passed", "version", version, "required", "v3.x")
	return nil
}

// checkServiceMeshVersionRequirement validates that if OpenShift ServiceMesh is installed,
// it must be version 3. ServiceMesh is optional, but if present, only v3 is allowed.
// Returns a StopError if ServiceMesh is installed with an incompatible version.
func checkServiceMeshVersionRequirement(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	logger := logf.FromContext(ctx)

	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	rr.Conditions.MarkUnknown(ServiceMeshVersionRequirement)

	// Check if ServiceMesh subscription exists
	serviceMeshExists, err := cluster.SubscriptionExists(ctx, rr.Client, serviceMeshOperatorSubscription)
	if err != nil {
		logger.Error(err, "Failed to check for ServiceMesh subscription")
		return setServiceMeshConditionAndStopError(k,
			reasonAPIError,
			"Unable to verify ServiceMesh installation due to API error",
			"Unable to verify ServiceMesh installation due to API error. "+
				"Cannot proceed with KServe deployment without confirming ServiceMesh compatibility. "+
				"Please check API access, RBAC permissions, and that the Subscription CRD is available.",
		)
	}

	if !serviceMeshExists {
		// No ServiceMesh installed - allow deployment (ServiceMesh is optional)
		conditions.SetStatusCondition(k, common.Condition{
			Type:    ServiceMeshVersionRequirement,
			Status:  metav1.ConditionTrue,
			Reason:  reasonServiceMeshNotInstalled,
			Message: "OpenShift ServiceMesh is not installed (optional)",
		})
		logger.Info("ServiceMesh not installed, allowing KServe deployment")
		return nil
	}

	// ServiceMesh IS installed - validate it's version 3
	return validateServiceMeshOperatorVersion(ctx, rr, k)
}

// checkPreConditions validates required and optional operator dependencies for KServe.
// Required: If ServiceMesh is installed, it must be version 3. Deployment is blocked otherwise.
// Optional: Checks for RHCL and LWS operators for LLMInferenceService features.
func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	// Check ServiceMesh version requirement (blocking check if incompatible version found)
	if err := checkServiceMeshVersionRequirement(ctx, rr); err != nil {
		return err
	}

	// Check for optional operators (informational only)
	rr.Conditions.MarkUnknown(LLMInferenceServiceDependencies)
	rr.Conditions.MarkUnknown(LLMInferenceServiceWideEPDependencies)

	rhclFound, err := cluster.SubscriptionExists(ctx, rr.Client, rhclOperatorSubscription)
	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("failed to check Red Hat Connectivity Link subscription: %w", err)
	}
	lwsFound, err := cluster.SubscriptionExists(ctx, rr.Client, lwsOperatorSubscription)
	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("failed to check Leader Worker Set subscription: %w", err)
	}
	// LLMInferenceService requires only the RHCL operator
	if rhclFound {
		conditions.SetStatusCondition(k, common.Condition{
			Type:   LLMInferenceServiceDependencies,
			Status: metav1.ConditionTrue,
		})
	} else {
		conditions.SetStatusCondition(k, common.Condition{
			Type:     LLMInferenceServiceDependencies,
			Status:   metav1.ConditionFalse,
			Reason:   subNotFound,
			Message:  "Warning: Red Hat Connectivity Link is not installed, LLMInferenceService cannot be used",
			Severity: common.ConditionSeverityInfo,
		})
	}
	// Wide Expert Parallelism requires both RHCL and LWS operators
	if rhclFound && lwsFound {
		conditions.SetStatusCondition(k, common.Condition{
			Type:   LLMInferenceServiceWideEPDependencies,
			Status: metav1.ConditionTrue,
		})
	} else {
		// Build message indicating which dependencies are missing
		var missing []string
		if !rhclFound {
			missing = append(missing, "Red Hat Connectivity Link")
		}
		if !lwsFound {
			missing = append(missing, "LeaderWorkerSet")
		}
		conditions.SetStatusCondition(k, common.Condition{
			Type:     LLMInferenceServiceWideEPDependencies,
			Status:   metav1.ConditionFalse,
			Reason:   subNotFound,
			Message:  fmt.Sprintf("Warning: %s not installed, Wide Expert Parallelism with LLMInferenceService cannot be used", strings.Join(missing, " and ")),
			Severity: common.ConditionSeverityInfo,
		})
	}
	return nil
}
