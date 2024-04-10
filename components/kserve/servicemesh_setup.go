package kserve

import (
	"fmt"
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServiceMesh(c client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	if dscispec.ServiceMesh.ManagementState == operatorv1.Managed && k.GetManagementState() == operatorv1.Managed {
		serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec, k.defineServiceMeshFeatures(c))
		return serviceMeshInitializer.Apply()
	}
	if dscispec.ServiceMesh.ManagementState == operatorv1.Unmanaged && k.GetManagementState() == operatorv1.Managed {
		return nil
	}

	return k.removeServiceMeshConfigurations(c, dscispec)
}

func (k *Kserve) removeServiceMeshConfigurations(cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec, k.defineServiceMeshFeatures(cli))
	return serviceMeshInitializer.Delete()
}

func (k *Kserve) defineServiceMeshFeatures(cli client.Client) feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		authorinoInstalled, err := deploy.ClusterSubscriptionExists(cli, "authorino-operator")
		if err != nil {
			return fmt.Errorf("failed to list subscriptions %w", err)
		}

		if authorinoInstalled {
			kserveExtAuthzErr := feature.CreateFeature("kserve-external-authz").
				For(handler).
				Manifests(
					path.Join(feature.KServeDir, "activator-envoyfilter.tmpl"),
					path.Join(feature.KServeDir, "envoy-oauth-temp-fix.tmpl"),
					path.Join(feature.KServeDir, "kserve-predictor-authorizationpolicy.tmpl"),
					path.Join(feature.KServeDir, "z-migrations"),
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
