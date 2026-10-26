package databaseservice

import (
	"context"
	"errors"
	"fmt"

	fwtypes "github.com/k8s-manifest-kit/engine/pkg/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.DatabaseServiceComponentName
	crName     = componentApi.DatabaseServiceInstanceName
	chartDir   = "databaseservice"

	// Rendered Deployment name from the db-operator Helm chart
	// (config/chart/templates/apps_v1_deployment.yaml).
	deploymentName = "odh-db-operator-operator"

	// controllerImage is the RELATED_IMAGE_* env var holding the digest-pinned
	// module operator image. Add a matching imageOverrides entry once the
	// quay image digest is published for release builds.
	controllerImage = "RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            moduleName,
				CRName:          crName,
				ChartDir:        chartDir,
				ReleaseName:     "opendatahub-db-operator",
				DeploymentName:  deploymentName,
				ControllerImage: controllerImage,
				GVK:             gvk.DatabaseService,
			},
		},
	}
}

// GetOperatorManifests sets Values.openshift.securityContextConstraints on
// OpenShift platforms so the db-operator chart renders the Internal Postgres
// SCC: the offline render lacks Capabilities, and the SCC API is absent on
// XKS (vanilla Kubernetes). Unknown platforms fail closed (no SCC) since
// rendering it on a non-OpenShift cluster fails the reconcile at apply time.
func (h *handler) GetOperatorManifests(platform *modules.PlatformContext) modules.OperatorManifests {
	result := h.BaseHandler.GetOperatorManifests(platform)
	if platform == nil || len(result.HelmCharts) == 0 {
		return result
	}

	switch platform.Release.Name {
	case cluster.OpenDataHub, cluster.SelfManagedRhoai, cluster.ManagedRhoai:
	default:
		return result
	}

	// Wrap the chart's Values func so the flag is injected at render time with
	// the renderer's context instead of eagerly with a detached one.
	base := result.HelmCharts[0].Values
	result.HelmCharts[0].Values = func(ctx context.Context) (fwtypes.Values, error) {
		vals := map[string]any{}
		if base != nil {
			v, err := base(ctx)
			if err != nil {
				return nil, fmt.Errorf("evaluating databaseservice chart values: %w", err)
			}
			if v != nil {
				vals = v
			}
		}
		// Merge instead of overwriting so base values under openshift.* survive.
		openshift, _ := vals["openshift"].(map[string]any)
		if openshift == nil {
			openshift = map[string]any{}
		}
		openshift["securityContextConstraints"] = true
		vals["openshift"] = openshift
		return vals, nil
	}
	return result
}

// DeleteOperatorResources cleans up with the same platform-gated values used at
// install. The BaseHandler default re-renders via the embedded handler, which
// bypasses the GetOperatorManifests override (no virtual dispatch in Go
// embedding) and would skip the SCC, leaving odh-db-internal-postgres behind.
func (h *handler) DeleteOperatorResources(ctx context.Context, cli client.Client, platform *modules.PlatformContext) error {
	return h.BaseHandler.DeleteOperatorManifests(ctx, cli, platform, h.GetOperatorManifests(platform))
}

func (h *handler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.DatabaseService.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.DatabaseService.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configApi.PlatformModules) bool {
	return modules != nil && modules.DatabaseService.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build DatabaseService CR")
	}

	// DatabaseServiceSpec is empty on the module CRD; do not project managementState
	// or invent DSC-only fields such as defaultProvider.
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{},
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}
