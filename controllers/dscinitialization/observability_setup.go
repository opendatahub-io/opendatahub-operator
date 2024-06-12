package dscinitialization

import (
	"context"
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

// +kubebuilder:rbac:groups="monitoring.rhobs",resources=servicemonitors,verbs=get;create;delete;update;watch;list;patch
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=podmonitors,verbs=get;create;delete;update;watch;list;patch

// +kubebuilder:rbac:groups="monitoring.rhobs",resources=prometheusrules,verbs=get;create;patch;update;deletecollection;delete
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=prometheuses,verbs=get;create;patch;update;deletecollection
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=prometheuses/finalizers,verbs=get;create;patch;delete;update
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=prometheuses/status,verbs=get;create;patch;delete;update

// +kubebuilder:rbac:groups="monitoring.rhobs",resources=alertmanagers,verbs=get;create;patch;delete;update;delete
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=alertmanagers/finalizers,verbs=get;create;patch;update;delete
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=alertmanagers/status,verbs=get;create;patch;update;delete
// +kubebuilder:rbac:groups="monitoring.rhobs",resources=alertmanagerconfigs,verbs=get;create;patch;update;delete

// +kubebuilder:rbac:groups="monitoring.rhobs",resources=monitoringstacks,verbs=get;create;patch;delete;deletec

// currently the logic is only called if it is for downstream.
func (r *DSCInitializationReconciler) configureObservability(ctx context.Context, instance *dsciv1.DSCInitialization) error {
	switch instance.Spec.Monitoring.ManagementState {
	case operatorv1.Managed:
		capabilities := []*feature.HandlerWithReporter[*dsciv1.DSCInitialization]{
			r.observabilityCapability(ctx, instance, oboCondition(status.ConfiguredReason, "CMO is configured")),
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
		if err := r.removeObservability(ctx, instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) removeObservability(ctx context.Context, instance *dsciv1.DSCInitialization) error {
	if instance.Spec.Monitoring.ManagementState == operatorv1.Managed {
		capabilities := []*feature.HandlerWithReporter[*dsciv1.DSCInitialization]{
			r.observabilityCapability(ctx, instance, oboCondition(status.RemovedReason, "ClusterObservability resources removed")),
		}

		for _, capability := range capabilities {
			capabilityErr := capability.Delete()
			if capabilityErr != nil {
				r.Log.Error(capabilityErr, "failed deleting ClusterObservability resources")
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed deleting ClusterObservability resources")

				return capabilityErr
			}
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) observabilityCapability(ctx context.Context, instance *dsciv1.DSCInitialization, initialCondition *conditionsv1.Condition) *feature.HandlerWithReporter[*dsciv1.DSCInitialization] { //nolint:lll // Reason: generics are long
	return feature.NewHandlerWithReporter(
		feature.ClusterFeaturesHandler(instance, r.observabilityCapabilityFeatures(ctx, instance)),
		createCapabilityReporter(r.Client, instance, initialCondition),
	)
}

func (r *DSCInitializationReconciler) observabilityCapabilityFeatures(ctx context.Context, instance *dsciv1.DSCInitialization) feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		commonOBOErr := feature.CreateFeature("create-obo-commonconfig").
			For(handler).
			ManifestsLocation(Templates.Location).
			Manifests(
				path.Join(Templates.AlertManageDir, "alertmanager-email.tmpl.yaml"), // dont wanna introduce one more FT so hook it here
				path.Join(Templates.CommonDir),
			).
			Load()

		if commonOBOErr != nil {
			return commonOBOErr
		}

		monitoringNamespace := instance.Spec.Monitoring.Namespace
		alertmanageErr := feature.CreateFeature("create-alertmanager").
			For(handler).
			ManifestsLocation(Templates.Location).
			Manifests(

				path.Join(Templates.AlertManageDir, "alertmanagerconfig.tmpl.yaml"),
			).
			WithData(obo.AlertmanagerDataValue). // fill in alertmanager data
			PreConditions(
				feature.EnsureOperatorIsInstalled("cluster-observability-operator"),
				feature.CreateNamespaceIfNotExists(instance.Spec.Monitoring.Namespace),
				feature.WaitForManagedSecret("redhat-rhods-deadmanssnitch", monitoringNamespace),
				feature.WaitForManagedSecret("redhat-rhods-pagerduty", monitoringNamespace),
				feature.WaitForManagedSecret("redhat-rhods-smtp", monitoringNamespace),
				feature.WaitForManagedConfigmap("rhoai-alertmanager-configmap", monitoringNamespace),
			).
			Load()

		if alertmanageErr != nil {
			return alertmanageErr
		}

		msErr := feature.CreateFeature("create-monitoringstack").
			For(handler).
			ManifestsLocation(Templates.Location).
			Manifests(
				path.Join(Templates.MonitoringStackDir),
			).
			Load()

		if msErr != nil {
			return msErr
		}

		platform, err := cluster.GetPlatform(r.Client)
		if err != nil {
			return fmt.Errorf("failed to determine platform (odh vs managed vs self-managed): %w", err)
		}
		if platform == cluster.ManagedRhods {
			// TODO: not sure if keep it or convert to WithResources
			err := cluster.UpdatePodSecurityRolebinding(ctx, r.Client, instance.Spec.Monitoring.Namespace, "redhat-ods-monitoring")
			if err != nil {
				return fmt.Errorf("failed updating monitoring security rolebinding: %w", err)
			}
			segmentCreationErr := feature.CreateFeature("create-segment-io").
				For(handler).
				ManifestsLocation(Templates.Location).
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
