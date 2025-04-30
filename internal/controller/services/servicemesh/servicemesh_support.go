package servicemesh

import (
	"context"
	"fmt"
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	dscictrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/manifest"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (r *ServiceMeshReconciler) configureServiceMesh(ctx context.Context, instance *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	serviceMeshManagementState := operatorv1.Removed
	if instance.Spec.ServiceMesh != nil {
		serviceMeshManagementState = instance.Spec.ServiceMesh.ManagementState
	} else {
		log.Info("ServiceMesh is not configured in DSCI, same as default to 'Removed'")
	}

	switch serviceMeshManagementState {
	case operatorv1.Managed:

		capabilities := []*feature.HandlerWithReporter[*dsciv1.DSCInitialization]{
			r.serviceMeshCapability(instance, serviceMeshCondition(status.ConfiguredReason, "Service Mesh configured")),
		}

		authzCapability, err := r.authorizationCapability(ctx, instance, authorizationCondition(status.ConfiguredReason, "Service Mesh Authorization configured"))
		if err != nil {
			return err
		}
		capabilities = append(capabilities, authzCapability)

		for _, capability := range capabilities {
			capabilityErr := capability.Apply(ctx, r.Client)
			if capabilityErr != nil {
				log.Error(capabilityErr, "failed applying service mesh resources")
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "ServiceMeshReconcileError", "failed applying service mesh resources")
				return capabilityErr
			}
		}

	case operatorv1.Unmanaged:
		log.Info("ServiceMesh CR is not configured by the operator, we won't do anything")
	case operatorv1.Removed:
		log.Info("existing ServiceMesh CR (owned by operator) will be removed")
		if err := r.removeServiceMesh(ctx, instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *ServiceMeshReconciler) removeServiceMesh(ctx context.Context, instance *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	// on condition of Managed, do not handle Removed when set to Removed it trigger DSCI reconcile to clean up
	if instance.Spec.ServiceMesh == nil {
		return nil
	}
	if instance.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		capabilities := []*feature.HandlerWithReporter[*dsciv1.DSCInitialization]{
			r.serviceMeshCapability(instance, serviceMeshCondition(status.RemovedReason, "Service Mesh removed")),
		}

		authzCapability, err := r.authorizationCapability(ctx, instance, authorizationCondition(status.RemovedReason, "Service Mesh Authorization removed"))
		if err != nil {
			return err
		}

		capabilities = append(capabilities, authzCapability)

		for _, capability := range capabilities {
			capabilityErr := capability.Delete(ctx, r.Client)
			if capabilityErr != nil {
				log.Error(capabilityErr, "failed deleting service mesh resources")
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "ServiceMeshReconcileError", "failed deleting service mesh resources")

				return capabilityErr
			}
		}
	}
	return nil
}

func (r *ServiceMeshReconciler) serviceMeshCapability(instance *dsciv1.DSCInitialization, initialCondition *common.Condition) *feature.HandlerWithReporter[*dsciv1.DSCInitialization] { //nolint:lll // Reason: generics are long
	return feature.NewHandlerWithReporter(
		feature.ClusterFeaturesHandler(instance, r.serviceMeshCapabilityFeatures(instance)),
		createCapabilityReporter(r.Client, instance, initialCondition),
	)
}

func (r *ServiceMeshReconciler) authorizationCapability(ctx context.Context, instance *dsciv1.DSCInitialization, condition *common.Condition) (*feature.HandlerWithReporter[*dsciv1.DSCInitialization], error) { //nolint:lll // Reason: generics are long
	authorinoInstalled, err := cluster.SubscriptionExists(ctx, r.Client, "authorino-operator")
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions %w", err)
	}

	if !authorinoInstalled {
		authzMissingOperatorCondition := &common.Condition{
			Type:    status.CapabilityServiceMeshAuthorization,
			Status:  metav1.ConditionFalse,
			Reason:  status.MissingOperatorReason,
			Message: "Authorino operator is not installed on the cluster, skipping authorization capability",
		}

		return feature.NewHandlerWithReporter(
			// EmptyFeaturesHandler acts as all the authorization features are disabled (calling Apply/Delete has no actual effect on the cluster)
			// but it's going to be reported as CapabilityServiceMeshAuthorization/MissingOperator condition/reason
			feature.EmptyFeaturesHandler,
			createCapabilityReporter(r.Client, instance, authzMissingOperatorCondition),
		), nil
	}

	return feature.NewHandlerWithReporter(
		feature.ClusterFeaturesHandler(instance, r.authorizationFeatures(instance)),
		createCapabilityReporter(r.Client, instance, condition),
	), nil
}

func (r *ServiceMeshReconciler) serviceMeshCapabilityFeatures(instance *dsciv1.DSCInitialization) feature.FeaturesProvider {
	return func(registry feature.FeaturesRegistry) error {
		controlPlaneSpec := instance.Spec.ServiceMesh.ControlPlane

		meshMetricsCollection := func(_ context.Context, _ client.Client, _ *feature.Feature) (bool, error) {
			return controlPlaneSpec.MetricsCollection == "Istio", nil
		}

		return registry.Add(
			feature.Define("mesh-control-plane-creation").
				Manifests(
					manifest.Location(dscictrl.Templates.Location).Include(
						path.Join(dscictrl.Templates.ServiceMeshDir),
					),
				).
				WithData(servicemesh.FeatureData.ControlPlane.Define(&instance.Spec).AsAction()).
				PreConditions(
					servicemesh.EnsureServiceMeshOperatorInstalled,
					feature.CreateNamespaceIfNotExists(controlPlaneSpec.Namespace),
				).
				PostConditions(
					feature.WaitForPodsToBeReady(controlPlaneSpec.Namespace),
				),
			feature.Define("mesh-metrics-collection").
				EnabledWhen(meshMetricsCollection).
				Manifests(
					manifest.Location(dscictrl.Templates.Location).
						Include(
							path.Join(dscictrl.Templates.MetricsDir),
						),
				).
				WithData(
					servicemesh.FeatureData.ControlPlane.Define(&instance.Spec).AsAction(),
				).
				PreConditions(
					servicemesh.EnsureServiceMeshInstalled,
				),
			feature.Define("mesh-shared-configmap").
				WithResources(servicemesh.MeshRefs, servicemesh.AuthRefs).
				WithData(
					servicemesh.FeatureData.ControlPlane.Define(&instance.Spec).AsAction(),
				).
				WithData(
					servicemesh.FeatureData.Authorization.All(&instance.Spec)...,
				),
		)
	}
}

func (r *ServiceMeshReconciler) authorizationFeatures(instance *dsciv1.DSCInitialization) feature.FeaturesProvider {
	return func(registry feature.FeaturesRegistry) error {
		serviceMeshSpec := instance.Spec.ServiceMesh

		return registry.Add(
			feature.Define("mesh-control-plane-external-authz").
				Manifests(
					manifest.Location(dscictrl.Templates.Location).
						Include(
							path.Join(dscictrl.Templates.AuthorinoDir, "auth-smm.tmpl.yaml"),
							path.Join(dscictrl.Templates.AuthorinoDir, "base"),
							path.Join(dscictrl.Templates.AuthorinoDir, "mesh-authz-ext-provider.patch.tmpl.yaml"),
						),
				).
				WithData(
					servicemesh.FeatureData.ControlPlane.Define(&instance.Spec).AsAction(),
				).
				WithData(
					servicemesh.FeatureData.Authorization.All(&instance.Spec)...,
				).
				PreConditions(
					feature.EnsureOperatorIsInstalled("authorino-operator"),
					servicemesh.EnsureServiceMeshInstalled,
					servicemesh.EnsureAuthNamespaceExists,
				).
				PostConditions(
					feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
				),

			// We do not have the control over deployment resource creation.
			// It is created by Authorino operator using Authorino CR and labels are not propagated from Authorino CR to spec.template
			// See https://issues.redhat.com/browse/RHOAIENG-5494 and https://github.com/Kuadrant/authorino-operator/pull/243
			//
			// To make it part of Service Mesh we have to patch it with injection
			// enabled instead, otherwise it will not have proxy pod injected.
			feature.Define("enable-proxy-injection-in-authorino-deployment").
				Manifests(
					manifest.Location(dscictrl.Templates.Location).
						Include(path.Join(dscictrl.Templates.AuthorinoDir, "deployment.injection.patch.tmpl.yaml")),
				).
				PreConditions(
					func(ctx context.Context, cli client.Client, f *feature.Feature) error {
						namespace, err := servicemesh.FeatureData.Authorization.Namespace.Extract(f)
						if err != nil {
							return fmt.Errorf("failed trying to resolve authorization provider namespace for feature '%s': %w", f.Name, err)
						}

						return feature.WaitForPodsToBeReady(namespace)(ctx, cli, f)
					},
				).
				WithData(servicemesh.FeatureData.ControlPlane.Define(&instance.Spec).AsAction()).
				WithData(servicemesh.FeatureData.Authorization.All(&instance.Spec)...),
		)
	}
}
