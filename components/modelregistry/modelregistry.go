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

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	_ "embed"
)

const DefaultModelRegistryCert = "default-modelregistry-cert"

var (
	ComponentName                   = "model-registry-operator"
	DefaultModelRegistriesNamespace = "odh-model-registries"
	Path                            = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odh"
	// we should not apply this label to the namespace, as it triggered namspace deletion during operator uninstall
	// modelRegistryLabels = cluster.WithLabels(
	//      labels.ODH.OwnedNamespace, "true",
	// ).
)

// Verifies that ModelRegistry implements ComponentInterface.
var _ components.ComponentInterface = (*ModelRegistry)(nil)

// ModelRegistry struct holds the configuration for the ModelRegistry component.
// The property `registriesNamespace` is immutable when `managementState` is `Managed`

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(self.managementState != 'Managed') || (oldSelf.registriesNamespace == '') || (oldSelf.managementState != 'Managed')|| (self.registriesNamespace == oldSelf.registriesNamespace)",message="RegistriesNamespace is immutable when model registry is Managed"
//nolint:lll

type ModelRegistry struct {
	components.Component `json:""`

	// Namespace for model registries to be installed, configurable only once when model registry is enabled, defaults to "odh-model-registries"
	// +kubebuilder:default="odh-model-registries"
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	RegistriesNamespace string `json:"registriesNamespace,omitempty"`
}

func (m *ModelRegistry) Init(ctx context.Context, _ cluster.Platform) error {
	log := logf.FromContext(ctx).WithName(ComponentName)

	var imageParamMap = map[string]string{
		"IMAGES_MODELREGISTRY_OPERATOR": "RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE",
		"IMAGES_GRPC_SERVICE":           "RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE",
		"IMAGES_REST_SERVICE":           "RELATED_IMAGE_ODH_MODEL_REGISTRY_IMAGE",
	}

	if err := deploy.ApplyParams(Path, imageParamMap); err != nil {
		log.Error(err, "failed to update image", "path", Path)
	}

	return nil
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

func (m *ModelRegistry) ReconcileComponent(ctx context.Context, cli client.Client,
	owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, platform cluster.Platform, _ bool) error {
	l := logf.FromContext(ctx)
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

		extraParamsMap := map[string]string{
			"DEFAULT_CERT": DefaultModelRegistryCert,
		}
		if err := deploy.ApplyParams(Path, nil, extraParamsMap); err != nil {
			return fmt.Errorf("failed to update image from %s : %w", Path, err)
		}

		// Create model registries namespace
		// We do not delete this namespace even when ModelRegistry is Removed or when operator is uninstalled.
		ns, err := cluster.CreateNamespace(ctx, cli, m.RegistriesNamespace)
		if err != nil {
			return err
		}
		l.Info("created model registry namespace", "namespace", m.RegistriesNamespace)
		// create servicemeshmember here, for now until post MVP solution
		err = enrollToServiceMesh(ctx, cli, dscispec, ns)
		if err != nil {
			return err
		}
		l.Info("created model registry servicemesh member", "namespace", m.RegistriesNamespace)
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
		// remove leftover kube-rbac-proxy container from older ODH installs
		if err := removeUnusedKubeRbacProxy(ctx, cli, m.GetComponentName(), dscispec.ApplicationsNamespace); err != nil {
			return fmt.Errorf("failed to remove kube-rbac-proxy from deployment for %s in namespace %s: %w", ComponentName, dscispec.ApplicationsNamespace, err)
		}
		if err := cluster.WaitForDeploymentAvailable(ctx, cli, m.GetComponentName(), dscispec.ApplicationsNamespace, 10, 1); err != nil {
			return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
		}
	}

	// CloudService Monitoring handling
	if platform == cluster.ManagedRhoai {
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

// removeUnusedKubeRbacProxy removes older kube-rbac-proxy container in mr operator deployment.
// This is required because patching a deployment using apply-patch doesn't remove older containers.
// See: https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/#notes-on-the-strategic-merge-patch
func removeUnusedKubeRbacProxy(ctx context.Context, cli client.Client, name string, namespace string) error {
	// get MR operator deployment
	componentDeploymentList := &appsv1.DeploymentList{}
	err := cli.List(ctx, componentDeploymentList, client.InNamespace(namespace), client.HasLabels{labels.ODH.Component(name)})
	if err != nil {
		return fmt.Errorf("error fetching list of deployments: %w", err)
	}
	nItems := len(componentDeploymentList.Items)
	if nItems != 1 {
		return fmt.Errorf("error fetching model registry operator deployment, found %d deployments", nItems)
	}

	// check if deployment has a kube-rbac-proxy container
	deployment := componentDeploymentList.Items[0]
	containers := deployment.Spec.Template.Spec.Containers
	// there should be a single container in latest deployment without kube-rbac-proxy
	if len(containers) == 1 {
		return nil
	}
	for i, container := range containers {
		if container.Name == "kube-rbac-proxy" {
			// remove if found
			return cli.Patch(ctx, &deployment, client.RawPatch(types.JSONPatchType,
				[]byte(fmt.Sprintf("[{\"op\": \"remove\", \"path\": \"/spec/template/spec/containers/%d\"}]", i))))
		}
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
