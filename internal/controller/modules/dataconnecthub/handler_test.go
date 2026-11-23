package dataconnecthub_test

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dataconnecthub"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func TestNewHandler(t *testing.T) {
	h := dataconnecthub.NewHandler()
	require.Equal(t, componentApi.DataConnectHubModuleName, h.GetName())
	require.Equal(t, componentApi.DataConnectHubInstanceName, h.GetCRName())
	require.Equal(t, gvk.DataConnectHub, h.GetGVK())
	require.Equal(t, "dc-controller-manager", h.GetDeploymentName())
	require.Equal(t, "RELATED_IMAGE_ODH_DATA_CONNECT_HUB_CONTROLLER_IMAGE", h.GetControllerImage())
	require.Equal(t, []string{
		"RELATED_IMAGE_ODH_DATA_CONNECT_HUB_REST_IMAGE",
		"RELATED_IMAGE_ODH_DATA_CONNECT_HUB_FLIGHT_IMAGE",
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
	}, h.GetRelatedImages())
	manifests := h.GetOperatorManifests(&modules.PlatformContext{ManifestsBasePath: "/manifests"})
	require.Len(t, manifests.Manifests, 1)
	require.Equal(t, "/manifests/dataconnecthub", manifests.Manifests[0].Path)
	require.Equal(t, "config/default", manifests.Manifests[0].SourcePath)
}

func TestPopulatePlatformModuleAndEnablement(t *testing.T) {
	h := dataconnecthub.NewHandler()
	for _, tt := range []struct {
		name  string
		state operatorv1.ManagementState
		want  string
	}{
		{name: "managed", state: "Managed", want: "Managed"},
		{name: "removed", state: "Removed", want: "Removed"},
		{name: "omitted defaults to removed", want: "Removed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dsc := &dscApi.DataScienceCluster{}
			dsc.Spec.Components.Data.DataConnectHub.ManagementState = tt.state
			pm := &configApi.PlatformModules{}
			h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: dsc})
			require.Equal(t, tt.want, string(pm.DataConnectHub.ManagementState))
			require.Equal(t, tt.want == "Managed", h.IsEnabled(pm))
		})
	}
}

func TestBuildModuleCRProjectsPlatformAndAuthentication(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, configv1.Install(scheme))
	auth := &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       configv1.AuthenticationSpec{ServiceAccountIssuer: "https://oidc.example/issuer"},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(auth).Build()
	dsc := &dscApi.DataScienceCluster{}
	release := common.Release{Name: cluster.OpenDataHub}
	release.Version = ofversion.OperatorVersion{Version: semver.MustParse("3.7.0")}

	u, err := dataconnecthub.NewHandler().BuildModuleCR(context.Background(), cli, &modules.DSCContext{DSC: dsc}, &modules.ModuleCRConfig{
		ApplicationsNamespace: "applications",
		Release:               release,
	})
	require.NoError(t, err)
	require.Equal(t, gvk.DataConnectHub, u.GroupVersionKind())
	require.Equal(t, componentApi.DataConnectHubInstanceName, u.GetName())
	require.Equal(t, map[string]any{
		"applicationsNamespace": "applications",
		"distribution": map[string]any{
			"name":    "OpenDataHub",
			"version": "3.7.0",
		},
		"gateway": map[string]any{
			"name":      gateway.GetDefaultGatewayName(),
			"namespace": gateway.GetGatewayNamespace(),
		},
		"tokenReviewAudiences": []string{"https://oidc.example/issuer"},
	}, u.Object["spec"])
}

func TestBuildModuleCRProjectsDSCOverrides(t *testing.T) {
	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.Data.DataConnectHub.Gateway = &componentApi.DataConnectHubGateway{
		Name:      "custom-gateway",
		Namespace: "custom-gateway-ns",
	}
	dsc.Spec.Components.Data.DataConnectHub.TokenReviewAudiences = []string{
		"https://issuer.example/custom",
	}
	u, err := dataconnecthub.NewHandler().BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSC: dsc}, &modules.ModuleCRConfig{
		ApplicationsNamespace: "applications",
	})
	require.NoError(t, err)
	spec, ok := u.Object["spec"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, map[string]any{
		"name":      "custom-gateway",
		"namespace": "custom-gateway-ns",
	}, spec["gateway"])
	require.Equal(t, []string{"https://issuer.example/custom"}, spec["tokenReviewAudiences"])
}

func TestBuildModuleCRRequiresDSCAndPlatformConfiguration(t *testing.T) {
	h := dataconnecthub.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, &modules.ModuleCRConfig{})
	require.ErrorContains(t, err, "DSC is nil")

	_, err = h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSC: &dscApi.DataScienceCluster{}}, nil)
	require.ErrorContains(t, err, "configuration is nil")

	_, err = h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSC: &dscApi.DataScienceCluster{}}, &modules.ModuleCRConfig{})
	require.ErrorContains(t, err, "applications namespace is empty")
}
