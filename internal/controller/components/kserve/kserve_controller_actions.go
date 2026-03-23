package kserve

import (
	"context"
	"errors"
	"fmt"

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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	LLMInferenceServiceConfigWellKnownAnnotationKey   = "serving.kserve.io/well-known-config"
	LLMInferenceServiceConfigWellKnownAnnotationValue = "true"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error { //nolint:unparam
	sourcePath := kserveManifestSourcePath
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		sourcePath = kserveManifestSourcePathXKS
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		kserveManifestInfo(sourcePath),
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

func customizeKserveConfigMap(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	//nolint:staticcheck
	serviceClusterIPNone := true
	if k.Spec.RawDeploymentServiceConfig == componentApi.KserveRawHeaded {
		// As default is Headless, only set false here if Headed is explicitly set
		serviceClusterIPNone = false
	}

	// Update ConfigMap (required for both OpenShift and XKS)
	kserveConfigMap := corev1.ConfigMap{}
	cmidx, err := getIndexedResource(rr.Resources, &kserveConfigMap, gvk.ConfigMap, kserveConfigMapName)
	if err != nil {
		return err
	}

	if err := updateInferenceCM(&kserveConfigMap, serviceClusterIPNone); err != nil {
		return err
	}

	if err = replaceResourceAtIndex(rr.Resources, cmidx, &kserveConfigMap); err != nil {
		return err
	}

	// Check if kserve-controller-manager deployment exists in resources
	// If not (e.g., XKS manifests), skip hash annotation
	kserveDeployment := appsv1.Deployment{}
	deployidx, err := getIndexedResource(rr.Resources, &kserveDeployment, gvk.Deployment, isvcControllerDeployment)
	if err != nil {
		// Only skip if deployment not found; propagate other errors
		if errors.Is(err, ErrResourceNotFound) {
			return nil
		}
		return err
	}

	// Add hash annotation to deployment to trigger restart on ConfigMap changes
	kserveConfigHash, err := hashConfigMap(&kserveConfigMap)
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

func checkOperatorAndCRDDependencies() actions.Fn {
	return dependency.NewAction(
		dependency.MonitorOperator(dependency.OperatorConfig{
			OperatorGVK: gvk.LeaderWorkerSetOperatorV1,
			Severity:    common.ConditionSeverityInfo,
			Filter:      lwsConditionFilter,
		}),
		// networking.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.DestinationRule,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.EnvoyFilter,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.IstioGateway,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.ProxyConfig,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.ServiceEntry,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.Sidecar,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.WorkloadEntry,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.WorkloadGroup,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// security.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.AuthorizationPolicy,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.PeerAuthentication,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.RequestAuthentication,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// telemetry.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.Telemetry,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// extensions.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.WasmPlugin,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// cert-manager.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerCertificate,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerCertificateRequest,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerIssuer,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerClusterIssuer,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// leaderworkerset.x-k8s.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.LeaderWorkerSetV1,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
	)
}

func checkSubscriptionDependencies() actions.Fn {
	return dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: LLMInferenceServiceDependencies,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: rhclOperatorSubscription, DisplayName: "Red Hat Connectivity Link"},
			},
			ClusterTypes: []string{cluster.ClusterTypeOpenShift},
			Reason:       subNotFound,
			Message:      "Warning: %s is not installed, LLMInferenceService cannot be used",
			Severity:     common.ConditionSeverityInfo,
		}),
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: LLMInferenceServiceWideEPDependencies,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: rhclOperatorSubscription, DisplayName: "Red Hat Connectivity Link"},
				{Name: lwsOperatorSubscription, DisplayName: "LeaderWorkerSet"},
			},
			ClusterTypes: []string{cluster.ClusterTypeOpenShift},
			Reason:       subNotFound,
			Message:      "Warning: %s not installed, Wide Expert Parallelism with LLMInferenceService cannot be used",
			Severity:     common.ConditionSeverityInfo,
		}),
	)
}
