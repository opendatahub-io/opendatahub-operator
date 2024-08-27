// Package modelregistry provides utility functions to config ModelRegistry, an ML Model metadata repository service
// +groupName=datasciencecluster.opendatahub.io
package modelregistry

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/platform/capabilities"

	_ "embed"
)

const DefaultModelRegistryCert = "default-modelregistry-cert"

var (
	ComponentName = "model-registry-operator"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odh"
	// we should not apply this label to the namespace, as it triggered namspace deletion during operator uninstall
	// modelRegistryLabels = cluster.WithLabels(
	//      labels.ODH.OwnedNamespace, "true",
	// ).
	ModelRegistriesNamespace = "odh-model-registries"
)

// Verifies that ModelRegistry implements ComponentInterface.
var _ components.ComponentInterface = (*ModelRegistry)(nil)
var _ capabilities.Injectable = (*ModelRegistry)(nil)

// ModelRegistry struct holds the configuration for the ModelRegistry component.
// +kubebuilder:object:generate=true
type ModelRegistry struct {
	components.Component `json:""`

	platform capabilities.PlatformCapabilitiesStruct `json:"-"`
}

func (m *ModelRegistry) OverrideManifests(ctx context.Context, _ cluster.Platform) error {
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

func (m *ModelRegistry) InjectPlatformCapabilities(platform capabilities.PlatformCapabilitiesStruct) {
	m.platform = platform
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
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed

	if enabled {
		// return error if ServiceMesh is not enabled, as it's a required feature
		if dscispec.ServiceMesh == nil || dscispec.ServiceMesh.ManagementState != operatorv1.Managed {
			return errors.New("ServiceMesh needs to be set to 'Managed' in DSCI CR, it is required by Model Registry")
		}

		if err := m.createDependencies(ctx, cli, dscispec); err != nil {
			return err
		}

		if m.DevFlags != nil {
			// Download manifests and update paths
			if err := m.OverrideManifests(ctx, platform); err != nil {
				return err
			}
		}

		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (m.DevFlags == nil || len(m.DevFlags.Manifests) == 0) {
			extraParamsMap := map[string]string{
				"DEFAULT_CERT": DefaultModelRegistryCert,
			}
			if err := deploy.ApplyParams(Path, imageParamMap, extraParamsMap); err != nil {
				return fmt.Errorf("failed to update image from %s : %w", Path, err)
			}
		}

		// Create model registries namespace
		// We do not delete this namespace even when ModelRegistry is Removed or when operator is uninstalled.
		ns, err := cluster.CreateNamespace(ctx, cli, ModelRegistriesNamespace)
		if err != nil {
			return err
		}
		l.Info("created model registry namespace", "namespace", ModelRegistriesNamespace)
		// create servicemeshmember here, for now until post MVP solution
		err = enrollToServiceMesh(ctx, cli, dscispec, ns)
		if err != nil {
			return err
		}
		l.Info("created model registry servicemesh member", "namespace", ModelRegistriesNamespace)

		m.platformRegister()
	} else {
		err := m.removeDependencies(ctx, cli, dscispec)
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

	if enabled {
		if err := cluster.WaitForDeploymentAvailable(ctx, cli, m.GetComponentName(), dscispec.ApplicationsNamespace, 10, 1); err != nil {
			return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
		}
	}

	// CloudService Monitoring handling
	if platform == cluster.ManagedRhods {
		if err := m.UpdatePrometheusConfig(cli, l, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err := deploy.DeployManifestsFromPath(ctx, cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
		l.Info("updating SRE monitoring done")
	}

	return nil
}

func (m *ModelRegistry) createDependencies(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	// create DefaultModelRegistryCert
	if err := cluster.PropagateDefaultIngressCertificate(ctx, cli, DefaultModelRegistryCert, dscispec.ServiceMesh.ControlPlane.Namespace); err != nil {
		return err
	}
	return nil
}

func (m *ModelRegistry) removeDependencies(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	// delete DefaultModelRegistryCert
	certSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultModelRegistryCert,
			Namespace: dscispec.ServiceMesh.ControlPlane.Namespace,
		},
	}
	// ignore error if the secret has already been removed
	if err := cli.Delete(ctx, &certSecret); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

//go:embed resources/servicemesh-member.tmpl.yaml
var smmTemplate string

func enrollToServiceMesh(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec, namespace *corev1.Namespace) error {
	tmpl, err := template.New("servicemeshmember").Parse(smmTemplate)
	if err != nil {
		return fmt.Errorf("error parsing servicemeshmember template: %w", err)
	}
	builder := strings.Builder{}
	controlPlaneData := struct {
		Namespace    string
		ControlPlane *infrav1.ControlPlaneSpec
	}{Namespace: namespace.Name, ControlPlane: &dscispec.ServiceMesh.ControlPlane}

	if err = tmpl.Execute(&builder, controlPlaneData); err != nil {
		return fmt.Errorf("error executing servicemeshmember template: %w", err)
	}

	unstrObj, err := conversion.StrToUnstructured(builder.String())
	if err != nil || len(unstrObj) != 1 {
		return fmt.Errorf("error converting servicemeshmember template: %w", err)
	}

	return client.IgnoreAlreadyExists(cli.Create(ctx, unstrObj[0]))
}
