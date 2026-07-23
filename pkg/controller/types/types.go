package types

import (
	fwtypes "github.com/opendatahub-io/odh-platform-utilities/framework/controller/types"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
)

type Controller = fwtypes.Controller

type ResourceObject = fwtypes.ResourceObject

type WithLogger = fwtypes.WithLogger

type ManifestInfo = fwtypes.ManifestInfo

type TemplateInfo = fwtypes.TemplateInfo

type HookFn = fwtypes.HookFn

type HelmChartInfo = fwtypes.HelmChartInfo

type ReconciliationRequest = fwtypes.ReconciliationRequest

var (
	Hash    = fwtypes.Hash
	HashStr = fwtypes.HashStr
)

// Extension keys for ODH-specific data on ReconciliationRequest.Extensions.
const (
	ExtKeyModuleEnvInjection = "odh.io/module-env-injection"
	ExtKeyDSCI               = "odh.io/dsci"
)

// OperatorCR identifies the custom resource created by the operator that
// this chart deploys. Used by the two-phase cleanup: when a dependency is
// set to Unmanaged, the CR is filtered from deploy so GC can delete it
// while operator resources are kept alive.
type OperatorCR struct {
	GVK       schema.GroupVersionKind
	Name      string
	Namespace string
}

// ModuleEnvInjection holds aggregated environment variable injection data
// for all enabled modules. Set by provisionModules and consumed by the
// injectModuleEnv action to inject RELATED_IMAGE_*, APPLICATIONS_NAMESPACE,
// MONITORING_NAMESPACE and platform identity env vars into module operator
// Deployments.
type ModuleEnvInjection struct {
	// PerModuleImages maps each module's related images to its chart/manifest
	// resources. Each entry's images are only injected into Deployments
	// rendered from that module's operator manifests.
	PerModuleImages []ModuleImages
	// ApplicationsNamespace is the platform's shared application namespace.
	ApplicationsNamespace string
	// MonitoringNamespace is the platform's monitoring namespace. "" when
	// monitoring is not configured or DSCI not exist (e.g xks).
	MonitoringNamespace string
	// PlatformType is the platform identifier (e.g. OpenDataHub,
	// SelfManagedRHOAI, XKS). Forwarded to module operators so they
	// can select platform-specific manifests without auto-detecting.
	PlatformType common.Platform
}

// ModuleImages associates a module's related images with a deployment name
// pattern so injection can be scoped to that module's operator Deployment.
type ModuleImages struct {
	// DeploymentName is the expected name of the module's operator Deployment.
	DeploymentName string
	// ContainerName is the target container within the Deployment.
	// Defaults to "manager" (the kubebuilder convention).
	ContainerName string
	// ControllerImage is the RELATED_IMAGE_* env var name whose value
	// replaces the target container's image field. Empty means no override.
	ControllerImage string
	// InitContainerName is the name of an init container whose image field
	// should also be overridden with the ControllerImage value.
	InitContainerName string
	// Images is the list of RELATED_IMAGE_* env var names for this module.
	Images []string
	// ExtraEnv is a fixed set of env vars injected directly into the target Deployment.
	ExtraEnv map[string]string
}

// SetModuleEnvInjection stores ModuleEnvInjection in the reconciliation request.
func SetModuleEnvInjection(rr *ReconciliationRequest, mei *ModuleEnvInjection) {
	if rr.Extensions == nil {
		rr.Extensions = make(map[string]any)
	}
	rr.Extensions[ExtKeyModuleEnvInjection] = mei
}

// GetModuleEnvInjection retrieves ModuleEnvInjection from the reconciliation request.
func GetModuleEnvInjection(rr *ReconciliationRequest) *ModuleEnvInjection {
	if rr.Extensions == nil {
		return nil
	}
	mei, _ := rr.Extensions[ExtKeyModuleEnvInjection].(*ModuleEnvInjection)
	return mei
}

// SetDSCI stores DSCInitialization in the reconciliation request.
func SetDSCI(rr *ReconciliationRequest, dsci *dsciv2.DSCInitialization) {
	if rr.Extensions == nil {
		rr.Extensions = make(map[string]any)
	}
	rr.Extensions[ExtKeyDSCI] = dsci
}

// GetDSCI retrieves DSCInitialization from the reconciliation request.
func GetDSCI(rr *ReconciliationRequest) *dsciv2.DSCInitialization {
	if rr.Extensions == nil {
		return nil
	}
	dsci, _ := rr.Extensions[ExtKeyDSCI].(*dsciv2.DSCInitialization)
	return dsci
}
