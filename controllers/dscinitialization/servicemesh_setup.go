package dscinitialization

import (
	"path"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

const templatesDir = "templates/servicemesh"

func (r *DSCInitializationReconciler) configureServiceMesh(instance *dsciv1.DSCInitialization) error {
	switch instance.Spec.ServiceMesh.ManagementState {
	case operatorv1.Managed:
		origin := featurev1.Origin{
			Type: featurev1.DSCIType,
			Name: instance.Name,
		}
		serviceMeshInitializer := feature.NewFeaturesInitializer(&instance.Spec, configureServiceMeshFeatures(&instance.Spec, origin))
		if err := serviceMeshInitializer.Prepare(); err != nil {
			r.Log.Error(err, "failed configuring service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed configuring service mesh resources")
			return err
		}

		if err := serviceMeshInitializer.Apply(); err != nil {
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
	// on condition of Managed, do not handle Removed when set to Removed it tigger DSCI reconcile to cleanup
	if instance.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		origin := featurev1.Origin{
			Type: featurev1.DSCIType,
			Name: instance.Name,
		}
		serviceMeshInitializer := feature.NewFeaturesInitializer(&instance.Spec, configureServiceMeshFeatures(&instance.Spec, origin))
		if err := serviceMeshInitializer.Prepare(); err != nil {
			r.Log.Error(err, "failed configuring service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed configuring service mesh resources")

			return err
		}

		if err := serviceMeshInitializer.Delete(); err != nil {
			r.Log.Error(err, "failed deleting service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed deleting service mesh resources")

			return err
		}
	}

	return nil
}

func configureServiceMeshFeatures(dscispec *dsciv1.DSCInitializationSpec, origin featurev1.Origin) feature.DefinedFeatures {
	return func(s *feature.FeaturesInitializer) error {
		var rootDir = filepath.Join(feature.BaseOutputDir, s.DSCInitializationSpec.ApplicationsNamespace)
		if err := feature.CopyEmbeddedFiles(templatesDir, rootDir); err != nil {
			return err
		}

		serviceMeshSpec := dscispec.ServiceMesh

		smcpCreation, errSmcp := feature.CreateFeature("mesh-control-plane-creation").
			For(dscispec, origin).
			Manifests(
				path.Join(rootDir, templatesDir, "base", "create-smcp.tmpl"),
			).
			PreConditions(
				servicemesh.EnsureServiceMeshOperatorInstalled,
				feature.CreateNamespaceIfNotExists(serviceMeshSpec.ControlPlane.Namespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
			).
			Load()
		if errSmcp != nil {
			return errSmcp
		}
		s.Features = append(s.Features, smcpCreation)

		noDefaultNetworkPolicies, errNp := feature.CreateFeature("mesh-control-plane-no-default-network-policies").
			For(dscispec, origin).
			Manifests(
				path.Join(rootDir, templatesDir, "base", "control-plane-disable-networkpolicies.patch.tmpl"),
			).
			Load()

		if errNp != nil {
			return errNp
		}
		s.Features = append(s.Features, noDefaultNetworkPolicies)

		if serviceMeshSpec.ControlPlane.MetricsCollection == "Istio" {
			metricsCollection, errMetrics := feature.CreateFeature("mesh-metrics-collection").
				For(dscispec, origin).
				Manifests(
					path.Join(rootDir, templatesDir, "metrics-collection"),
				).
				PreConditions(
					servicemesh.EnsureServiceMeshInstalled,
				).
				Load()
			if errMetrics != nil {
				return errMetrics
			}
			s.Features = append(s.Features, metricsCollection)
		}
		return nil
	}
}
