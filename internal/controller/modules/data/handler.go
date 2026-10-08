package data

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName             = componentApi.DataModuleName
	crName                 = componentApi.FeastOperatorInstanceName
	chartDir               = componentApi.DataModuleName
	dataReadyConditionType = "DataReady"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:              moduleName,
				CRName:            crName,
				ReleaseName:       "opendatahub-feast-operator",
				ChartDir:          chartDir,
				NamespaceValueKey: "namespace",
				ControllerImage:   "RELATED_IMAGE_ODH_FEAST_MODULE_OPERATOR_IMAGE",
				InitContainerName: "copy-manifests",
				GVK:               gvk.FeastOperator,
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
					"RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE",
				},
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}

	data := dscCtx.DSC.Spec.Components.Data
	switch {
	case data.FeatureStore.ManagementState == operatorv1.Managed:
		pm.Data.ManagementState = operatorv1.Managed
	case data.DataRegistry.ManagementState == operatorv1.Managed:
		pm.Data.ManagementState = operatorv1.Managed
	default:
		pm.Data.ManagementState = operatorv1.Removed
	}
}

func (h *handler) IsEnabled(modules *configApi.PlatformModules) bool {
	return modules != nil && modules.Data.ManagementState == operatorv1.Managed
}

func (h *handler) GetReadyConditionType() string {
	return dataReadyConditionType
}

// WriteDSCComponentStatus writes the Data module's aggregate status
// to the renamed Data field in v3 DSC status.
func (h *handler) WriteDSCComponentStatus(
	dsc *dscApi.DataScienceCluster,
	enabled bool,
	releases []common.ComponentRelease,
) {
	if dsc == nil {
		return
	}

	managementState := operatorv1.Removed
	if enabled {
		managementState = operatorv1.Managed
	}

	dataStatus := &dsc.Status.Components.Data
	dataStatus.ManagementState = managementState
	if dataStatus.DataCommonStatus == nil && len(releases) > 0 {
		dataStatus.DataCommonStatus = &componentApi.DataCommonStatus{}
	}
	if dataStatus.DataCommonStatus != nil {
		dataStatus.Releases = releases
	}
}

// BuildModuleCR constructs the FeastOperator CR with OIDC settings and
// capability projection from the platform context.
func (h *handler) BuildModuleCR(
	ctx context.Context,
	cli client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if cli == nil {
		return nil, errors.New("kubernetes client is nil, cannot resolve OIDC for FeastOperator CR")
	}

	spec := map[string]any{}

	// --- OIDC (existing) ---
	oidcSpec, err := getGatewayOIDCSpec(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve OIDC for FeastOperator CR: %w", err)
	}
	if oidcSpec != nil {
		spec["oidc"] = map[string]any{
			"issuerURL": oidcSpec.IssuerURL,
		}
	}

	// --- Capabilities ---
	if dscCtx != nil && dscCtx.DSC != nil {
		data := dscCtx.DSC.Spec.Components.Data

		fsState := string(data.FeatureStore.ManagementState)
		if fsState == "" {
			fsState = string(operatorv1.Removed)
		}
		drState := string(data.DataRegistry.ManagementState)
		if drState == "" {
			drState = string(operatorv1.Removed)
		}

		spec["capabilities"] = map[string]any{
			"featureStore": map[string]any{
				"managementState": fsState,
			},
			"dataRegistry": map[string]any{
				"managementState": drState,
			},
		}
	}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}

// getGatewayOIDCSpec returns the OIDC issuer URL from GatewayConfig when the
// cluster uses external OIDC. Returns nil when OpenShift OAuth is used or
// GatewayConfig is not yet provisioned.
func getGatewayOIDCSpec(ctx context.Context, cli client.Client) (*oidcResult, error) {
	authMode, err := cluster.GetClusterAuthenticationMode(ctx, cli)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to detect cluster authentication mode: %w", err)
	}

	if authMode != cluster.AuthModeOIDC {
		return nil, nil
	}

	gc := serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, client.ObjectKey{Name: serviceApi.GatewayConfigName}, &gc); err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get GatewayConfig: %w", err)
	}

	if gc.Spec.OIDC == nil || gc.Spec.OIDC.IssuerURL == "" {
		return nil, nil
	}

	parsed, err := url.ParseRequestURI(gc.Spec.OIDC.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid OIDC issuer URL in GatewayConfig: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("OIDC issuer URL must be an absolute https URL, got %q", gc.Spec.OIDC.IssuerURL)
	}

	return &oidcResult{IssuerURL: parsed.String()}, nil
}

type oidcResult struct {
	IssuerURL string
}
