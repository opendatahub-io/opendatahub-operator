package ossm

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	kfapisv3 "github.com/opendatahub-io/opendatahub-operator/apis"
	kftypesv3 "github.com/opendatahub-io/opendatahub-operator/apis/apps"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/ossm/feature"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig/ossmplugin"
	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"path"
	"path/filepath"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	PluginName = "KfOssmPlugin"
)

var log = ctrlLog.Log.WithName(PluginName)

type OssmInstaller struct {
	*kfconfig.KfConfig
	PluginSpec *ossmplugin.OssmPluginSpec
	config     *rest.Config
	features   []*feature.Feature
}

func NewOssmInstaller(kfConfig *kfconfig.KfConfig, restConfig *rest.Config) *OssmInstaller {
	return &OssmInstaller{
		KfConfig: kfConfig,
		config:   restConfig,
	}

}

// GetPlatform returns the ossm kfapp. It's called by coordinator.GetPlatform
func GetPlatform(kfConfig *kfconfig.KfConfig) (kftypesv3.Platform, error) {
	return NewOssmInstaller(kfConfig, kftypesv3.GetConfig()), nil
}

// GetPluginSpec gets the plugin spec.
func (o *OssmInstaller) GetPluginSpec() (*ossmplugin.OssmPluginSpec, error) {
	if o.PluginSpec != nil {
		return o.PluginSpec, nil
	}

	o.PluginSpec = &ossmplugin.OssmPluginSpec{}
	if err := o.KfConfig.GetPluginSpec(PluginName, o.PluginSpec); err != nil {
		return nil, err
	}

	// Populate target Kubeflow namespace to have it in one struct instead
	o.PluginSpec.AppNamespace = o.KfConfig.Namespace

	return o.PluginSpec, nil
}

// Init performs validation of the plugin spec and ensures all resources,
// such as features and their templates, are processed and initialized.
func (o *OssmInstaller) Init(_ kftypesv3.ResourceEnum) error {
	if o.KfConfig.Spec.SkipInitProject {
		log.Info("Skipping init phase", "plugin", PluginName)
	}

	log.Info("Initializing", "plugin", PluginName)
	pluginSpec, err := o.GetPluginSpec()
	if err != nil {
		return internalError(errors.WithStack(err))
	}

	if err := pluginSpec.SetDefaults(); err != nil {
		return internalError(errors.WithStack(err))
	}

	if valid, reason := pluginSpec.IsValid(); !valid {
		return internalError(errors.New(reason))
	}

	if err := o.SyncCache(); err != nil {
		return internalError(err)
	}

	if err := o.addServiceMeshOverlays(); err != nil {
		return internalError(err)
	}

	if err := o.addOssmEnvFile("USE_ISTIO", "true", "ISTIO_GATEWAY", fmt.Sprintf("%s/%s", pluginSpec.AppNamespace, "odh-gateway")); err != nil {
		return internalError(err)
	}

	return o.enableFeatures()
}

func (o *OssmInstaller) enableFeatures() error {

	var rootDir = filepath.Join(feature.BaseOutputDir, o.Namespace, o.Name)
	if err := copyEmbeddedFiles("templates", rootDir); err != nil {
		return internalError(errors.WithStack(err))
	}

	if smcpInstallation, err := feature.CreateFeature("control-plane-install-managed").
		For(o.PluginSpec).
		UsingConfig(o.config).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "control-plane-minimal.tmpl"),
		).
		EnabledIf(func(f *feature.Feature) bool {
			return f.Spec.Mesh.InstallationMode != ossmplugin.PreInstalled
		}).
		Preconditions(
			feature.EnsureCRDIsInstalled("servicemeshcontrolplanes.maistra.io"),
			feature.CreateNamespace(o.PluginSpec.Mesh.Namespace),
		).
		Postconditions(
			feature.WaitForControlPlaneToBeReady,
		).
		Load(); err != nil {
		return err
	} else {
		o.features = append(o.features, smcpInstallation)
	}

	if oauth, err := feature.CreateFeature("control-plane-configure-oauth").
		For(o.PluginSpec).
		UsingConfig(o.config).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "base"),
			path.Join(rootDir, feature.ControlPlaneDir, "oauth"),
			path.Join(rootDir, feature.ControlPlaneDir, "filters"),
		).
		WithResources(
			feature.SelfSignedCertificate,
			feature.EnvoyOAuthSecrets,
		).
		WithData(feature.ClusterDetails, feature.OAuthConfig).
		Preconditions(
			feature.EnsureServiceMeshInstalled,
		).
		Postconditions(
			feature.WaitForPodsToBeReady(o.PluginSpec.Mesh.Namespace),
		).
		OnDelete(
			feature.RemoveOAuthClient,
			feature.RemoveTokenVolumes,
		).Load(); err != nil {
		return err
	} else {
		o.features = append(o.features, oauth)
	}

	if cfMaps, err := feature.CreateFeature("shared-config-maps").
		For(o.PluginSpec).
		UsingConfig(o.config).
		WithResources(feature.ConfigMaps).
		Load(); err != nil {
		return err
	} else {
		o.features = append(o.features, cfMaps)
	}

	if serviceMesh, err := feature.CreateFeature("app-add-namespace-to-service-mesh").
		For(o.PluginSpec).
		UsingConfig(o.config).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "smm.tmpl"),
			path.Join(rootDir, feature.ControlPlaneDir, "namespace.patch.tmpl"),
		).
		WithData(feature.ClusterDetails).
		Load(); err != nil {
		return err
	} else {
		o.features = append(o.features, serviceMesh)
	}

	if dashboard, err := feature.CreateFeature("app-enable-service-mesh-in-dashboard").
		For(o.PluginSpec).
		UsingConfig(o.config).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "routing"),
		).
		WithResources(feature.ServiceMeshEnabledInDashboard).
		WithData(feature.ClusterDetails).
		Postconditions(
			feature.WaitForPodsToBeReady(o.PluginSpec.Mesh.Namespace),
		).
		Load(); err != nil {
		return err
	} else {
		o.features = append(o.features, dashboard)
	}

	if dataScienceProjects, err := feature.CreateFeature("app-migrate-data-science-projects").
		For(o.PluginSpec).
		UsingConfig(o.config).
		WithResources(feature.MigratedDataScienceProjects).
		Load(); err != nil {
		return err
	} else {
		o.features = append(o.features, dataScienceProjects)
	}

	if extAuthz, err := feature.CreateFeature("control-plane-setup-external-authorization").
		For(o.PluginSpec).
		UsingConfig(o.config).
		Manifests(
			path.Join(rootDir, feature.AuthDir, "auth-smm.tmpl"),
			path.Join(rootDir, feature.AuthDir, "base"),
			path.Join(rootDir, feature.AuthDir, "rbac"),
			path.Join(rootDir, feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"),
		).
		WithData(feature.ClusterDetails).
		Preconditions(
			feature.CreateNamespace(o.PluginSpec.Auth.Namespace),
			feature.EnsureCRDIsInstalled("authconfigs.authorino.kuadrant.io"),
			feature.EnsureServiceMeshInstalled,
		).
		Postconditions(
			feature.WaitForPodsToBeReady(o.PluginSpec.Mesh.Namespace),
			feature.WaitForPodsToBeReady(o.PluginSpec.Auth.Namespace),
		).
		OnDelete(feature.RemoveExtensionProvider).
		Load(); err != nil {
		return err
	} else {
		o.features = append(o.features, extAuthz)
	}

	return nil
}

// Apply is implemented as a patched logic in coordinator.go similar to already existing GCP one.
// Plugins called from within the Kubernetes are not invoked in this particular phase by default.
// In order to prevent the accumulation of unmanaged resources in the event of a failure,
// the use of Generate() alone is not viable. This is due to its consequence of prematurely halting
// the Delete() chain without invoking our associated Delete hook.
func (o *OssmInstaller) Apply(_ kftypesv3.ResourceEnum) error {
	var applyErrors *multierror.Error

	for _, f := range o.features {
		err := f.Apply()
		applyErrors = multierror.Append(applyErrors, err)
	}

	return applyErrors.ErrorOrNil()
}

// Delete is implemented as a patched logic in coordinator.go similar to the Apply().
// Plugins called from within the Kubernetes are not invoked in this particular phase by default.
// Because we create resources we need to clean them up on deletion of KfDef though.
func (o *OssmInstaller) Delete(_ kftypesv3.ResourceEnum) error {
	var cleanupErrors *multierror.Error
	// Performs cleanups in reverse order (stack), this way e.g. we can unpatch SMCP before deleting it (if managed)
	// Though it sounds unnecessary it keeps features isolated and there is no need to rely on the InstallationMode
	// between the features when it comes to clean-up. This is based on the assumption, that features
	// are created in the correct order or are self-contained.
	for i := len(o.features) - 1; i >= 0; i-- {
		log.Info("cleanup", "name", o.features[i].Name)
		cleanupErrors = multierror.Append(cleanupErrors, o.features[i].Cleanup())
	}

	return cleanupErrors.ErrorOrNil()
}

// Generate function is not utilized by the plugin. This is because, in the event of an error, using Generate()
// would lead to the complete omission of the Delete() call.
// This, in turn, would leave resources created by it dangling in the cluster.
func (o *OssmInstaller) Generate(_ kftypesv3.ResourceEnum) error {
	return nil
}

// Dump is unused. Plugins called from within the Kubernetes are not invoked in this particular phase by default.
func (o *OssmInstaller) Dump(_ kftypesv3.ResourceEnum) error {
	return nil
}

func internalError(err error) error {
	return &kfapisv3.KfError{
		Code:    int(kfapisv3.INTERNAL_ERROR),
		Message: fmt.Sprintf("%+v", err),
	}
}
