package kserve

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const DefaultCertificateSecretName = "knative-serving-cert"

var (
	imageParamMap = map[string]string{
		"kserve-agent":                     "RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE",
		"kserve-controller":                "RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
		"kserve-router":                    "RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE",
		"kserve-storage-initializer":       "RELATED_IMAGE_ODH_KSERVE_STORAGE_INITIALIZER_IMAGE",
		"kserve-llm-d":                     "RELATED_IMAGE_RHAIIS_VLLM_CUDA_IMAGE",
		"kserve-llm-d-inference-scheduler": "RELATED_IMAGE_ODH_LLM_D_INFERENCE_SCHEDULER_IMAGE",
		"kserve-llm-d-routing-sidecar":     "RELATED_IMAGE_ODH_LLM_D_ROUTING_SIDECAR_IMAGE",
		"oauth-proxy":                      "RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE",
	}
)

//go:embed resources
var resourcesFS embed.FS

var isRequiredOperators = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		return false
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return strings.HasPrefix(e.Object.GetName(), serverlessOperator) || strings.HasPrefix(e.Object.GetName(), serviceMeshOperator)
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return strings.HasPrefix(e.Object.GetName(), serverlessOperator) || strings.HasPrefix(e.Object.GetName(), serviceMeshOperator)
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}

func kserveManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: componentName,
		SourcePath: sourcePath,
	}
}

func createServingCertResource(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec, kserve *componentApi.Kserve) error {
	domain := getKnativeDomain(ctx, cli, kserve)
	secretName := getKnativeCertSecretName(kserve)

	switch kserve.Spec.Serving.IngressGateway.Certificate.Type {
	case infrav1.SelfSigned:
		return cluster.CreateSelfSignedCertificate(ctx, cli, secretName,
			domain, dscispec.ServiceMesh.ControlPlane.Namespace,
			cluster.OwnedBy(kserve, cli.Scheme()))
	case infrav1.Provided:
		return nil
	case infrav1.OpenshiftDefaultIngress:
		return cluster.PropagateDefaultIngressCertificate(ctx, cli, secretName, dscispec.ServiceMesh.ControlPlane.Namespace)
	default:
		return ErrServerlessUnsupportedCertType
	}
}

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return nil, fmt.Errorf("resource instance %v is not a componentApi.Kserve)", rr.Instance)
	}

	knativeIngressDomain := getKnativeDomain(ctx, rr.Client, k)
	knativeCertificateSecret := getKnativeCertSecretName(k)

	return map[string]any{
		"AuthExtensionName":        rr.DSCI.Spec.ApplicationsNamespace + "-auth-provider",
		"ControlPlane":             rr.DSCI.Spec.ServiceMesh.ControlPlane,
		"KnativeCertificateSecret": knativeCertificateSecret,
		"KnativeIngressDomain":     knativeIngressDomain,
		"Serving":                  k.Spec.Serving,
	}, nil
}

func getKnativeDomain(ctx context.Context, cli client.Client, k *componentApi.Kserve) string {
	domain := k.Spec.Serving.IngressGateway.Domain
	if domain != "" {
		return domain
	}

	domain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return ""
	}
	domain = "*." + domain
	return domain
}

func getKnativeCertSecretName(k *componentApi.Kserve) string {
	name := k.Spec.Serving.IngressGateway.Certificate.SecretName
	if name == "" {
		name = DefaultCertificateSecretName
	}

	return name
}

func getDefaultDeploymentMode(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) (string, error) {
	kserveConfigMap := corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{Name: kserveConfigMapName, Namespace: dscispec.ApplicationsNamespace}, &kserveConfigMap)
	if k8serr.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	deployConfig, err := getDeployConfig(&kserveConfigMap)
	if err != nil {
		return "", err
	}

	return deployConfig.DefaultDeploymentMode, nil
}

func updateInferenceCM(inferenceServiceConfigMap *corev1.ConfigMap, defaultmode componentApi.DefaultDeploymentMode, isHeadless bool) error {
	deployData, err := getDeployConfig(inferenceServiceConfigMap)
	if err != nil {
		return err
	}

	if deployData.DefaultDeploymentMode != string(defaultmode) {
		// deploy
		deployData.DefaultDeploymentMode = string(defaultmode)
		deployDataBytes, err := json.MarshalIndent(deployData, "", " ")
		if err != nil {
			return fmt.Errorf("could not set values in configmap %s. %w", kserveConfigMapName, err)
		}
		inferenceServiceConfigMap.Data[DeployConfigName] = string(deployDataBytes)

		// ingress
		var ingressData map[string]interface{}
		if err = json.Unmarshal([]byte(inferenceServiceConfigMap.Data[IngressConfigKeyName]), &ingressData); err != nil {
			return fmt.Errorf("error retrieving value for key '%s' from configmap %s. %w", IngressConfigKeyName, kserveConfigMapName, err)
		}
		if defaultmode == componentApi.RawDeployment {
			ingressData["disableIngressCreation"] = true
		} else {
			ingressData["disableIngressCreation"] = false
		}
		ingressDataBytes, err := json.MarshalIndent(ingressData, "", " ")
		if err != nil {
			return fmt.Errorf("could not set values in configmap %s. %w", kserveConfigMapName, err)
		}
		inferenceServiceConfigMap.Data[IngressConfigKeyName] = string(ingressDataBytes)
	}

	// service
	var serviceData map[string]interface{}
	if err := json.Unmarshal([]byte(inferenceServiceConfigMap.Data[ServiceConfigKeyName]), &serviceData); err != nil {
		return fmt.Errorf("error retrieving value for key '%s' from configmap %s. %w", ServiceConfigKeyName, kserveConfigMapName, err)
	}
	serviceData["serviceClusterIPNone"] = isHeadless
	serviceDataBytes, err := json.MarshalIndent(serviceData, "", " ")
	if err != nil {
		return fmt.Errorf("could not set values in configmap %s. %w", kserveConfigMapName, err)
	}
	inferenceServiceConfigMap.Data[ServiceConfigKeyName] = string(serviceDataBytes)
	return nil
}

func getIndexedResource(rs []unstructured.Unstructured, obj any, g schema.GroupVersionKind, name string) (int, error) {
	var idx = -1
	for i, r := range rs {
		if r.GroupVersionKind() == g && r.GetName() == name {
			idx = i
			break
		}
	}

	if idx == -1 {
		return -1, fmt.Errorf("could not find %T with name %v in resources list", obj, name)
	}

	err := runtime.DefaultUnstructuredConverter.FromUnstructured(rs[idx].Object, obj)
	if err != nil {
		return idx, fmt.Errorf("failed converting to %T from unstructured %v: %w", obj, rs[idx].Object, err)
	}

	return idx, nil
}

func replaceResourceAtIndex(rs []unstructured.Unstructured, idx int, obj any) error {
	u, err := resources.ToUnstructured(obj)
	if err != nil {
		return err
	}

	rs[idx] = *u
	return nil
}

func hashConfigMap(cm *corev1.ConfigMap) (string, error) {
	u, err := resources.ToUnstructured(cm)
	if err != nil {
		return "", err
	}

	h, err := resources.Hash(u)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(h), nil
}

// shouldRemoveOwnerRefAndLabel encapsulates the decision on whether a resource
// should be excluded from GC collection because it's considered Unmanaged.
func shouldRemoveOwnerRefAndLabel(
	dsciServiceMesh *infrav1.ServiceMeshSpec,
	kserveServing infrav1.ServingSpec,
	res unstructured.Unstructured,
) bool {
	switch {
	case isForDependency("servicemesh")(&res):
		return dsciServiceMesh != nil && dsciServiceMesh.ManagementState == operatorv1.Unmanaged
	case isForDependency("serverless")(&res):
		if dsciServiceMesh != nil && dsciServiceMesh.ManagementState == operatorv1.Unmanaged {
			return true
		}
		if kserveServing.ManagementState == operatorv1.Unmanaged {
			return true
		}
	}
	return false
}

func getAndRemoveOwnerReferences(
	ctx context.Context,
	cli client.Client,
	res unstructured.Unstructured,
	predicate func(or metav1.OwnerReference) bool,
) error {
	current := resources.GvkToUnstructured(res.GroupVersionKind())

	lookupErr := cli.Get(ctx, client.ObjectKeyFromObject(&res), current)
	if k8serr.IsNotFound(lookupErr) {
		return nil
	}
	if errors.Is(lookupErr, &meta.NoKindMatchError{}) {
		return nil
	}
	if lookupErr != nil {
		return fmt.Errorf("failed to lookup object %s/%s: %w",
			res.GetNamespace(), res.GetName(), lookupErr)
	}

	ls := current.GetLabels()
	maps.DeleteFunc(ls, func(k string, v string) bool {
		return k == labels.PlatformPartOf &&
			v == strings.ToLower(componentApi.KserveKind)
	})
	current.SetLabels(ls)

	return resources.RemoveOwnerReferences(ctx, cli, current, predicate)
}

func isForDependency(s string) func(u *unstructured.Unstructured) bool {
	return func(u *unstructured.Unstructured) bool {
		for k, v := range u.GetLabels() {
			if k == labels.PlatformDependency && v == s {
				return true
			}
		}
		return false
	}
}

func isKserveOwnerRef(or metav1.OwnerReference) bool {
	return or.APIVersion == componentApi.GroupVersion.String() &&
		or.Kind == componentApi.KserveKind
}

// deleteResourceIfOwnedByKserve fetches a cluster resource and deletes it only if it has a Kserve OwnerReference.
// This prevents accidentally deleting resources created by users or other controllers.
func deleteResourceIfOwnedByKserve(ctx context.Context, cli client.Client, res unstructured.Unstructured, logger logr.Logger) error {
	// Fetch the cluster resource to check ownership
	key := client.ObjectKeyFromObject(&res)
	clusterRes := resources.GvkToUnstructured(res.GroupVersionKind())

	if err := cli.Get(ctx, key, clusterRes); err != nil {
		if k8serr.IsNotFound(err) {
			return nil // Already deleted or never existed
		}
		if errors.Is(err, &meta.NoKindMatchError{}) {
			return nil // CRD missing
		}
		return odherrors.NewStopErrorW(err)
	}

	// Only delete if resource has Kserve OwnerReference
	hasKserveOwner := false
	for _, owner := range clusterRes.GetOwnerReferences() {
		if isKserveOwnerRef(owner) {
			hasKserveOwner = true
			break
		}
	}

	if !hasKserveOwner {
		return nil // Skip resources not owned by Kserve controller
	}

	err := cli.Delete(ctx, clusterRes, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		if errors.Is(err, &meta.NoKindMatchError{}) {
			return nil
		}
		return odherrors.NewStopErrorW(err)
	}
	logger.Info("Deleted", "kind", clusterRes.GetKind(), "name", clusterRes.GetName(), "namespace", clusterRes.GetNamespace())
	return nil
}
