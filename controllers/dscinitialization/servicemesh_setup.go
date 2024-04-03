package dscinitialization

import (
	"context"
	"fmt"
	"path"
	"reflect"

	operatorv1 "github.com/openshift/api/operator/v1"
	authentication "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

var (
	// Default value of audiences for DSCI.SM.auth.
	defaultAudiences = []string{"https://kubernetes.default.svc"}
	smSetupLog       = ctrl.Log.WithName("setup")
)

func (r *DSCInitializationReconciler) configureServiceMesh(instance *dsciv1.DSCInitialization) error {
	switch instance.Spec.ServiceMesh.ManagementState {
	case operatorv1.Managed:
		serviceMeshFeatures := feature.ClusterFeaturesHandler(instance, r.configureServiceMeshFeatures())

		if err := serviceMeshFeatures.Apply(); err != nil {
			r.Log.Error(err, "failed applying service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed applying service mesh resources")
			return err
		}
	case operatorv1.Unmanaged:
		r.Log.Info("ServiceMesh CR is not configured by the operator, we won't do anything")
	case operatorv1.Removed:
		r.Log.Info("existing ServiceMesh CR (owned by operator) will be removed")
		if err := r.removeServiceMesh(instance); err != nil {
			return err
		}
	}
	return nil
}

func (r *DSCInitializationReconciler) removeServiceMesh(instance *dsciv1.DSCInitialization) error {
	// on condition of Managed, do not handle Removed when set to Removed it trigger DSCI reconcile to clean up
	if instance.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		serviceMeshFeatures := feature.ClusterFeaturesHandler(instance, r.configureServiceMeshFeatures())

		if err := serviceMeshFeatures.Delete(); err != nil {
			r.Log.Error(err, "failed deleting service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed deleting service mesh resources")

			return err
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) configureServiceMeshFeatures() feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		serviceMeshSpec := handler.DSCInitializationSpec.ServiceMesh

		smcpCreationErr := feature.CreateFeature("mesh-control-plane-creation").
			For(handler).
			Manifests(
				path.Join(feature.ServiceMeshDir, "base", "create-smcp.tmpl"),
			).
			PreConditions(
				servicemesh.EnsureServiceMeshOperatorInstalled,
				feature.CreateNamespaceIfNotExists(serviceMeshSpec.ControlPlane.Namespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
			).
			Load()

		if smcpCreationErr != nil {
			return smcpCreationErr
		}

		if serviceMeshSpec.ControlPlane.MetricsCollection == "Istio" {
			metricsCollectionErr := feature.CreateFeature("mesh-metrics-collection").
				For(handler).
				PreConditions(
					servicemesh.EnsureServiceMeshInstalled,
				).
				Manifests(
					path.Join(feature.ServiceMeshDir, "metrics-collection"),
				).
				Load()
			if metricsCollectionErr != nil {
				return metricsCollectionErr
			}
		}

		cfgMapErr := feature.CreateFeature("mesh-shared-configmap").
			For(handler).
			WithResources(servicemesh.MeshRefs, servicemesh.AuthRefs(definedAudiencesOrDefault(handler.ServiceMesh.Auth.Audiences))).
			Load()
		if cfgMapErr != nil {
			return cfgMapErr
		}

		extAuthzErr := feature.CreateFeature("mesh-control-plane-external-authz").
			For(handler).
			Manifests(
				path.Join(feature.AuthDir, "auth-smm.tmpl"),
				path.Join(feature.AuthDir, "base"),
				path.Join(feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"),
			).
			WithData(servicemesh.ClusterDetails).
			PreConditions(
				feature.EnsureOperatorIsInstalled("authorino-operator"),
				servicemesh.EnsureServiceMeshInstalled,
				servicemesh.EnsureAuthNamespaceExists,
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
				func(f *feature.Feature) error {
					return feature.WaitForPodsToBeReady(handler.DSCInitializationSpec.ServiceMesh.Auth.Namespace)(f)
				},
				func(f *feature.Feature) error {
					// We do not have the control over deployment resource creation.
					// It is created by Authorino operator using Authorino CR
					//
					// To make it part of Service Mesh we have to patch it with injection
					// enabled instead, otherwise it will not have proxy pod injected.
					return f.ApplyManifest(path.Join(feature.AuthDir, "deployment.injection.patch.tmpl"))
				},
			).
			OnDelete(servicemesh.RemoveExtensionProvider).
			Load()
		if extAuthzErr != nil {
			return extAuthzErr
		}

		return nil
	}
}

func isDefaultAudiences(specAudiences *[]string) bool {
	return specAudiences == nil || reflect.DeepEqual(*specAudiences, defaultAudiences)
}

// definedAudiencesOrDefault returns the default audiences if the provided audiences are default, otherwise it returns the provided audiences.
func definedAudiencesOrDefault(specAudiences *[]string) []string {
	if isDefaultAudiences(specAudiences) {
		return fetchClusterAudiences()
	}
	return *specAudiences
}

func fetchClusterAudiences() []string {
	restCfg, err := config.GetConfig()
	if err != nil {
		smSetupLog.Error(err, "Error getting config, using default audiences")
		return defaultAudiences
	}

	tokenReview := &authentication.TokenReview{
		Spec: authentication.TokenReviewSpec{
			Token: restCfg.BearerToken,
		},
	}

	tokenReviewClient, err := client.New(restCfg, client.Options{})
	if err != nil {
		smSetupLog.Error(err, "Error creating client, using default audiences")
		return defaultAudiences
	}

	if err = tokenReviewClient.Create(context.Background(), tokenReview, &client.CreateOptions{}); err != nil {
		smSetupLog.Error(err, "Error creating TokenReview, using default audiences")
		return defaultAudiences
	}

	if tokenReview.Status.Error != "" || !tokenReview.Status.Authenticated {
		smSetupLog.Error(fmt.Errorf(tokenReview.Status.Error), "Error with token review authentication status, using default audiences")
		return defaultAudiences
	}

	return tokenReview.Status.Audiences
}
