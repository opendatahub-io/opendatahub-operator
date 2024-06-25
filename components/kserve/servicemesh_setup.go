package kserve

import (
	"context"
	"fmt"
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServiceMesh(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	if dscispec.ServiceMesh != nil {
		if dscispec.ServiceMesh.ManagementState == operatorv1.Managed && k.GetManagementState() == operatorv1.Managed {
			serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec, k.defineServiceMeshFeatures(ctx, cli))
			return serviceMeshInitializer.Apply(ctx)
		}
		if dscispec.ServiceMesh.ManagementState == operatorv1.Unmanaged && k.GetManagementState() == operatorv1.Managed {
			return nil
		}
	}

	return k.removeServiceMeshConfigurations(ctx, cli, dscispec)
}

func (k *Kserve) removeServiceMeshConfigurations(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec, k.defineServiceMeshFeatures(ctx, cli))
	return serviceMeshInitializer.Delete(ctx)
}

func (k *Kserve) defineServiceMeshFeatures(ctx context.Context, cli client.Client) feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		authorinoInstalled, err := cluster.SubscriptionExists(ctx, cli, "authorino-operator")
		if err != nil {
			return fmt.Errorf("failed to list subscriptions %w", err)
		}

		if authorinoInstalled {
			kserveExtAuthzErr := feature.CreateFeature("kserve-external-authz").
				For(handler).
				ManifestsLocation(Resources.Location).
				Manifests(
					path.Join(Resources.ServiceMeshDir, "activator-envoyfilter.tmpl.yaml"),
					path.Join(Resources.ServiceMeshDir, "envoy-oauth-temp-fix.tmpl.yaml"),
					path.Join(Resources.ServiceMeshDir, "kserve-predictor-authorizationpolicy.tmpl.yaml"),
					path.Join(Resources.ServiceMeshDir, "z-migrations"),
				).
				WithData(servicemesh.ClusterDetails).
				Load()

			if kserveExtAuthzErr != nil {
				return kserveExtAuthzErr
			}
		} else {
			fmt.Println("WARN: Authorino operator is not installed on the cluster, skipping authorization capability")
		}

		return nil
	}
}
