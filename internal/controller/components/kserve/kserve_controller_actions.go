package kserve

import (
	"context"
	"fmt"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

	sailOperatorIgnoreAnnotation = "sailoperator.io/ignore"

	istioSidecarInjectorWebhook = "istio-sidecar-injector"
	istioValidatorWebhook       = "istio-validator-istio-system"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error { //nolint:unparam
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

func customizeKserveConfigMap(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
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

// annotateIstioWebhooks works around a sail-operator bug (OSSM-12397) where webhook
// configuration updates trigger an infinite Helm reinstall loop on vanilla Kubernetes.
// It adds the sailoperator.io/ignore=true annotation to the two webhooks that istiod
// creates (istio-sidecar-injector and istio-validator-istio-system), telling the
// sail-operator to stop watching them.
//
// TODO(OSSM-12397): Remove this workaround once the sail-operator ships a fix.
// Tracking: https://issues.redhat.com/browse/RHOAIENG-52246
//
// Only runs on xKS platforms where the sail-operator is used.
// Annotation failures are logged but do not block reconciliation, since this
// is a workaround rather than a core requirement.
func annotateIstioWebhooks(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.Release.Name != cluster.XKS {
		return nil
	}

	logger := logf.FromContext(ctx)

	// Errors are intentionally not returned here because this is a temporary
	// workaround (see TODO above). Failing to annotate a webhook should not
	// block KServe reconciliation; the annotation will be retried on the next
	// reconciliation loop.
	if err := ensureSailOperatorIgnoreAnnotation(
		ctx, rr.Client, istioSidecarInjectorWebhook, &admissionregistrationv1.MutatingWebhookConfiguration{},
	); err != nil {
		logger.Error(err, "Failed to annotate webhook (non-fatal)", "name", istioSidecarInjectorWebhook)
	}

	if err := ensureSailOperatorIgnoreAnnotation(
		ctx, rr.Client, istioValidatorWebhook, &admissionregistrationv1.ValidatingWebhookConfiguration{},
	); err != nil {
		logger.Error(err, "Failed to annotate webhook (non-fatal)", "name", istioValidatorWebhook)
	}

	return nil
}

func ensureSailOperatorIgnoreAnnotation(ctx context.Context, c client.Client, name string, obj client.Object) error {
	logger := logf.FromContext(ctx)

	if err := c.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return err
	}

	annotations := obj.GetAnnotations()
	if annotations[sailOperatorIgnoreAnnotation] == "true" {
		return nil
	}

	annotationPatch := client.RawPatch(types.MergePatchType,
		[]byte(`{"metadata":{"annotations":{"`+sailOperatorIgnoreAnnotation+`":"true"}}}`))

	if err := c.Patch(ctx, obj, annotationPatch); err != nil {
		return err
	}

	logger.Info("Annotated webhook with sailoperator.io/ignore=true",
		"kind", obj.GetObjectKind().GroupVersionKind().Kind,
		"name", name,
	)

	return nil
}

// checkPreConditions checks if there are optional operators that KServe could use.
func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

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
