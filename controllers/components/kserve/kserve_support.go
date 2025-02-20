package kserve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/manifest"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func kserveManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       deploy.DefaultManifestPath,
		ContextDir: componentName,
		SourcePath: sourcePath,
	}
}

func configureServerlessFeatures(dsciSpec *dsciv1.DSCInitializationSpec, kserve *componentApi.Kserve) feature.FeaturesProvider {
	return func(registry feature.FeaturesRegistry) error {
		servingDeployment := feature.Define("serverless-serving-deployment").
			Manifests(
				manifest.Location(Resources.Location).
					Include(
						path.Join(Resources.InstallDir),
					),
			).
			WithData(
				serverless.FeatureData.IngressDomain.Define(&kserve.Spec.Serving).AsAction(),
				serverless.FeatureData.Serving.Define(&kserve.Spec.Serving).AsAction(),
				servicemesh.FeatureData.ControlPlane.Define(dsciSpec).AsAction(),
			).
			PreConditions(
				serverless.EnsureServerlessOperatorInstalled,
				serverless.EnsureServerlessAbsent,
				servicemesh.EnsureServiceMeshInstalled,
				feature.CreateNamespaceIfNotExists(serverless.KnativeServingNamespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serverless.KnativeServingNamespace),
			)

		istioSecretFiltering := feature.Define("serverless-net-istio-secret-filtering").
			Manifests(
				manifest.Location(Resources.Location).
					Include(
						path.Join(Resources.BaseDir, "serving-net-istio-secret-filtering.patch.tmpl.yaml"),
					),
			).
			WithData(serverless.FeatureData.Serving.Define(&kserve.Spec.Serving).AsAction()).
			PreConditions(serverless.EnsureServerlessServingDeployed).
			PostConditions(
				feature.WaitForPodsToBeReady(serverless.KnativeServingNamespace),
			)

		servingGateway := feature.Define("serverless-serving-gateways").
			Manifests(
				manifest.Location(Resources.Location).
					Include(
						path.Join(Resources.GatewaysDir),
					),
			).
			WithData(
				serverless.FeatureData.IngressDomain.Define(&kserve.Spec.Serving).AsAction(),
				serverless.FeatureData.CertificateName.Define(&kserve.Spec.Serving).AsAction(),
				serverless.FeatureData.Serving.Define(&kserve.Spec.Serving).AsAction(),
				servicemesh.FeatureData.ControlPlane.Define(dsciSpec).AsAction(),
			).
			WithResources(serverless.ServingCertificateResource).
			PreConditions(serverless.EnsureServerlessServingDeployed)

		return registry.Add(
			servingDeployment,
			istioSecretFiltering,
			servingGateway,
		)
	}
}

func defineServiceMeshFeatures(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) feature.FeaturesProvider {
	return func(registry feature.FeaturesRegistry) error {
		authorinoInstalled, err := cluster.SubscriptionExists(ctx, cli, "authorino-operator")
		if err != nil {
			return fmt.Errorf("failed to list subscriptions %w", err)
		}

		if authorinoInstalled {
			kserveExtAuthzErr := registry.Add(feature.Define("kserve-external-authz").
				Manifests(
					manifest.Location(Resources.Location).
						Include(
							path.Join(Resources.ServiceMeshDir, "activator-envoyfilter.tmpl.yaml"),
							path.Join(Resources.ServiceMeshDir, "envoy-oauth-temp-fix.tmpl.yaml"),
							path.Join(Resources.ServiceMeshDir, "kserve-predictor-authorizationpolicy.tmpl.yaml"),
							path.Join(Resources.ServiceMeshDir, "kserve-inferencegraph-envoyfilter.tmpl.yaml"),
							path.Join(Resources.ServiceMeshDir, "kserve-inferencegraph-authorizationpolicy.tmpl.yaml"),
						),
				).
				Managed().
				WithData(
					feature.Entry("Domain", cluster.GetDomain),
					servicemesh.FeatureData.ControlPlane.Define(dscispec).AsAction(),
				).
				WithData(
					servicemesh.FeatureData.Authorization.All(dscispec)...,
				),
			)

			if kserveExtAuthzErr != nil {
				return kserveExtAuthzErr
			}
		} else {
			ctrl.Log.Info("WARN: Authorino operator is not installed on the cluster, skipping authorization capability")
		}

		return nil
	}
}

func removeServiceMeshConfigurations(ctx context.Context, cli client.Client, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec) error {
	serviceMeshInitializer := feature.ComponentFeaturesHandler(owner, componentName, dscispec.ApplicationsNamespace, defineServiceMeshFeatures(ctx, cli, dscispec))
	return serviceMeshInitializer.Delete(ctx, cli)
}

func removeServerlessFeatures(ctx context.Context, cli client.Client, k *componentApi.Kserve, dscispec *dsciv1.DSCInitializationSpec) error {
	serverlessFeatures := feature.ComponentFeaturesHandler(k, componentName, dscispec.ApplicationsNamespace, configureServerlessFeatures(dscispec, k))
	return serverlessFeatures.Delete(ctx, cli)
}

func getDefaultDeploymentMode(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) (string, error) {
	kserveConfigMap := corev1.ConfigMap{}
	if err := cli.Get(ctx, client.ObjectKey{Name: kserveConfigMapName, Namespace: dscispec.ApplicationsNamespace}, &kserveConfigMap); err != nil {
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

func ownedViaFT(cli client.Client) handler.MapFunc {
	return func(ctx context.Context, a client.Object) []reconcile.Request {
		for _, or := range a.GetOwnerReferences() {
			if or.Kind == "FeatureTracker" {
				ft := featuresv1.FeatureTracker{}
				if err := cli.Get(ctx, client.ObjectKey{Name: or.Name}, &ft); err != nil {
					return []reconcile.Request{}
				}

				for _, ftor := range ft.GetOwnerReferences() {
					if ftor.Kind == componentApi.KserveKind && ftor.Name != "" {
						return []reconcile.Request{{
							NamespacedName: types.NamespacedName{
								Name: ftor.Name,
							},
						}}
					}
				}
			}
		}

		return []reconcile.Request{}
	}
}

func isLegacyOwnerRef(or metav1.OwnerReference) bool {
	return or.APIVersion == gvk.DataScienceCluster.GroupVersion().String() && or.Kind == gvk.DataScienceCluster.Kind
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
