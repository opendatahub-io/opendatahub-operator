package kserve

import (
	"context"
	"fmt"
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/manifest"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServiceMesh(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	if dscispec.ServiceMesh != nil {
		if dscispec.ServiceMesh.ManagementState == operatorv1.Managed && k.GetManagementState() == operatorv1.Managed {
			serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec.ApplicationsNamespace, k.defineServiceMeshFeatures(ctx, cli, dscispec))
			return serviceMeshInitializer.Apply(ctx)
		}
		if dscispec.ServiceMesh.ManagementState == operatorv1.Unmanaged && k.GetManagementState() == operatorv1.Managed {
			return nil
		}
	}

	return k.removeServiceMeshConfigurations(ctx, cli, dscispec)
}

func (k *Kserve) removeServiceMeshConfigurations(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec.ApplicationsNamespace, k.defineServiceMeshFeatures(ctx, cli, dscispec))
	return serviceMeshInitializer.Delete(ctx)
}

func (k *Kserve) defineServiceMeshFeatures(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) feature.FeaturesProvider {
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
							path.Join(Resources.ServiceMeshDir, "z-migrations"),
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
