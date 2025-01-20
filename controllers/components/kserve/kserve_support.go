package kserve

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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
		Path:       deploy.DefaultManifestPath,
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
	default:
		return cluster.PropagateDefaultIngressCertificate(ctx, cli,
			secretName, dscispec.ServiceMesh.ControlPlane.Namespace)
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
	return domain
}

func getKnativeCertSecretName(k *componentApi.Kserve) string {
	name := k.Spec.Serving.IngressGateway.Certificate.SecretName
	if name == "" {
		name = "knative-serving-cert"
	}

	return name
}

func getDefaultDeploymentMode(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) (string, error) {
	kserveConfigMap := corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{Name: kserveConfigMapName, Namespace: dscispec.ApplicationsNamespace}, &kserveConfigMap)
	if errors.IsNotFound(err) {
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

func setDefaultDeploymentMode(inferenceServiceConfigMap *corev1.ConfigMap, defaultmode componentApi.DefaultDeploymentMode) error {
	deployData, err := getDeployConfig(inferenceServiceConfigMap)
	if err != nil {
		return err
	}

	if deployData.DefaultDeploymentMode != string(defaultmode) {
		deployData.DefaultDeploymentMode = string(defaultmode)
		deployDataBytes, err := json.MarshalIndent(deployData, "", " ")
		if err != nil {
			return fmt.Errorf("could not set values in configmap %s. %w", kserveConfigMapName, err)
		}
		inferenceServiceConfigMap.Data[DeployConfigName] = string(deployDataBytes)

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

func ifGVKInstalled(kvg schema.GroupVersionKind) func(context.Context, *odhtypes.ReconciliationRequest) bool {
	return func(ctx context.Context, rr *odhtypes.ReconciliationRequest) bool {
		hasCRD, err := cluster.HasCRD(ctx, rr.Client, kvg)
		if err != nil {
			ctrl.Log.Error(err, "error checking if CRD installed", "GVK", kvg)
			return false
		}
		return hasCRD
	}
}
