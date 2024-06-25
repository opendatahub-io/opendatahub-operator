// Package modelregistry provides utility functions to config ModelRegistry, an ML Model metadata repository service
// +groupName=datasciencecluster.opendatahub.io
package modelregistry

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName = "model-registry-operator"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odh"
	// we should not apply this label to the namespace, as it triggered namspace deletion during operator uninstall
	// modelRegistryLabels = cluster.WithLabels(
	// 	labels.ODH.OwnedNamespace, "true",
	// ).
)

// Verifies that ModelRegistry implements ComponentInterface.
var _ components.ComponentInterface = (*ModelRegistry)(nil)

// ModelRegistry struct holds the configuration for the ModelRegistry component.
// +kubebuilder:object:generate=true
type ModelRegistry struct {
	components.Component `json:""`
}

func (m *ModelRegistry) OverrideManifests(ctx context.Context, _ string) error {
	// If devflags are set, update default manifests path
	if len(m.DevFlags.Manifests) != 0 {
		manifestConfig := m.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "overlays/odh"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}

	return nil
}

func (m *ModelRegistry) GetComponentName() string {
	return ComponentName
}

func (m *ModelRegistry) ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger,
	owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, platform cluster.Platform, _ bool) error {
	l := m.ConfigComponentLogger(logger, ComponentName, dscispec)
	var imageParamMap = map[string]string{
		"IMAGES_MODELREGISTRY_OPERATOR": "RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE",
		"IMAGES_GRPC_SERVICE":           "RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE",
		"IMAGES_REST_SERVICE":           "RELATED_IMAGE_ODH_MODEL_REGISTRY_IMAGE",
	}
	enabled := m.GetManagementState() == operatorv1.Managed

	if enabled {
		if m.DevFlags != nil {
			// Download manifests and update paths
			if err := m.OverrideManifests(ctx, string(platform)); err != nil {
				return err
			}
		}

		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (m.DevFlags == nil || len(m.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, imageParamMap, false); err != nil {
				return fmt.Errorf("failed to update image from %s : %w", Path, err)
			}
		}

		// Create odh-model-registries namespace
		// We do not delete this namespace even when ModelRegistry is Removed or when operator is uninstalled.
		_, err := cluster.CreateNamespace(ctx, cli, "odh-model-registries")
		if err != nil {
			return err
		}
	}
	// Deploy ModelRegistry Operator
	if err := deploy.DeployManifestsFromPath(ctx, cli, owner, Path, dscispec.ApplicationsNamespace, m.GetComponentName(), enabled); err != nil {
		return err
	}
	l.Info("apply manifests done")

	// Create additional model registry resources, componentEnabled=true because these extras are never deleted!
	if err := deploy.DeployManifestsFromPath(ctx, cli, owner, Path+"/extras", dscispec.ApplicationsNamespace, m.GetComponentName(), true); err != nil {
		return err
	}
	l.Info("apply extra manifests done")

	return nil
}
