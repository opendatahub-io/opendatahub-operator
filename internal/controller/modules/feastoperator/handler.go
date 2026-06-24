package feastoperator

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	moduleName = componentApi.FeastOperatorComponentName
	crName     = componentApi.FeastOperatorInstanceName
	chartDir   = "opendatahub-feast-operator"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:        moduleName,
				CRName:      crName,
				ReleaseName: "opendatahub-feast-operator",
				ChartDir:    chartDir,
				GVK: schema.GroupVersionKind{
					Group:   "components.platform.opendatahub.io",
					Version: "v1",
					Kind:    componentApi.FeastOperatorKind,
				},
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
					"RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE",
				},
			},
		},
	}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.FeastOperator.ManagementState == operatorv1.Managed
	}
	return false
}

// BuildModuleCR constructs the FeastOperator CR with OIDC settings projected
// from the platform context when the cluster uses external OIDC.
func (h *handler) BuildModuleCR(
	ctx context.Context,
	cli client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build FeastOperator CR")
	}

	spec := map[string]any{}

	if cli != nil {
		oidcSpec, err := getGatewayOIDCSpec(ctx, cli)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve OIDC for FeastOperator CR: %w", err)
		}
		if oidcSpec != nil {
			spec["oidc"] = map[string]any{
				"issuerURL": oidcSpec.IssuerURL,
			}
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
