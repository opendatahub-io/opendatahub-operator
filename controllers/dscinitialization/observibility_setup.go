package dscinitialization

import (
	"fmt"
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/obo"
)

// WEN: below are for the new CMO
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=monitoringstack,verbs=get;create;delete;update;patch
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=alertmanager,verbs=get;create;delete;update;patch
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=alertmanagerconfig,verbs=get;create;delete;update;patch
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=prometheusrule,verbs=get;create;delete;update;patch
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=servicemonitor,verbs=get;create;delete;update;patch
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=podmonitor,verbs=get;create;delete;update;patch

// currently the logic is only called if it is for downstream.
func (r *DSCInitializationReconciler) configureObservability(instance *dsciv1.DSCInitialization) error {
	switch instance.Spec.Monitoring.ManagementState {
	case operatorv1.Managed:
		capabilities := []*feature.HandlerWithReporter[*dsciv1.DSCInitialization]{
			r.observabilityCapability(instance, oboCondition(status.ConfiguredReason, "CMO is configured")),
		}
		for _, capability := range capabilities {
			capabilityErr := capability.Apply()
			if capabilityErr != nil {
				r.Log.Error(capabilityErr, "failed applying ClusterObservability resources")
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed applying ClusterObservability")
				return capabilityErr
			}
		}
	case operatorv1.Removed:
		r.Log.Info("existing ClusterObservability CR (owned by operator) will be removed")
		if err := r.removeObservability(instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) removeObservability(instance *dsciv1.DSCInitialization) error {
	if instance.Spec.Monitoring.ManagementState == operatorv1.Managed {
		capabilities := []*feature.HandlerWithReporter[*dsciv1.DSCInitialization]{
			r.observabilityCapability(instance, oboCondition(status.RemovedReason, "ClusterObservability resources removed")),
		}

		for _, capability := range capabilities {
			capabilityErr := capability.Delete()
			if capabilityErr != nil {
				r.Log.Error(capabilityErr, "failed deleting Clusterobservability resources")
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed deleting Clusterobservability resources")

				return capabilityErr
			}
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) observabilityCapability(instance *dsciv1.DSCInitialization, initialCondition *conditionsv1.Condition) *feature.HandlerWithReporter[*dsciv1.DSCInitialization] { //nolint:lll // Reason: generics are long
	return feature.NewHandlerWithReporter(
		feature.ClusterFeaturesHandler(instance, r.observabilityCapabilityFeatures(instance)),
		createCapabilityReporter(r.Client, instance, initialCondition),
	)
}

func (r *DSCInitializationReconciler) observabilityCapabilityFeatures(instance *dsciv1.DSCInitialization) feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		monitoringNamespace := instance.Spec.Monitoring.Namespace
		alertmanageErr := feature.CreateFeature("create-alertmanager").
			For(handler).
			ManifestSource(Templates.Source).
			Manifests(
				path.Join(Templates.AlertManageDir),
			).
			Managed().                           // we want to reconcie this by oepartor in Managed Cluster, do we want for ODH too?
			WithData(obo.AlertmanagerDataValue). // fill in alertmanager data
			PreConditions(
				feature.EnsureOperatorIsInstalled("cluster-observability-operator"),
				feature.CreateNamespaceIfNotExists(instance.Spec.Monitoring.Namespace),
				feature.WaitForManagedSecret("redhat-rhods-deadmanssnitch", monitoringNamespace),
				feature.WaitForManagedSecret("redhat-rhods-pagerduty", monitoringNamespace),
				feature.WaitForManagedSecret("redhat-rhods-smtp", monitoringNamespace),
			).
			Load()

		if alertmanageErr != nil {
			return alertmanageErr
		}

		msErr := feature.CreateFeature("create-monitoringstack").
			For(handler).
			ManifestSource(Templates.Source).
			Manifests(
				path.Join(Templates.MonitoringStackDir),
			).
			Load()
		if msErr != nil {
			return msErr
		}

		commonOBOErr := feature.CreateFeature("create-obo-commonconfig").
			For(handler).
			ManifestSource(Templates.Source).
			Manifests(
				path.Join(Templates.CommonDir),
			).
			Load()

		if commonOBOErr != nil {
			return commonOBOErr
		}

		platform, err := cluster.GetPlatform(r.Client)
		if err != nil {
			return fmt.Errorf("failed to determine platform (odh vs managed vs self-managed): %w", err)
		}
		if platform == cluster.ManagedRhods {
			// TODO: not sure if keep it or convert to WithResources
			err := cluster.UpdatePodSecurityRolebinding(r.Client, instance.Spec.Monitoring.Namespace, "redhat-ods-monitoring")
			if err != nil {
				return fmt.Errorf("failed updating monitoring security rolebinding: %w", err)
			}
			segmentCreationErr := feature.CreateFeature("create-segment-io").
				For(handler).
				ManifestSource(Templates.Source).
				Manifests(
					path.Join(Templates.SegmentDir),
				).
				Load()
			if segmentCreationErr != nil {
				return segmentCreationErr
			}
		}

		return err
	}
}
