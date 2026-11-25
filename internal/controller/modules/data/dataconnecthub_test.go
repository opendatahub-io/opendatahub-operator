package data_test

import (
	"context"
	"fmt"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/data"
)

func TestDataConnectHubUsesSharedDataModule(t *testing.T) {
	states := []operatorv1.ManagementState{operatorv1.Managed, operatorv1.Removed}
	for _, fs := range states {
		for _, registry := range states {
			for _, dch := range states {
				t.Run(fmt.Sprintf("featureStore=%s/dataRegistry=%s/dataConnectHub=%s", fs, registry, dch), func(t *testing.T) {
					dsc := &dscApi.DataScienceCluster{}
					dsc.Spec.Components.Data.FeatureStore.ManagementState = fs
					dsc.Spec.Components.Data.DataRegistry.ManagementState = registry
					dsc.Spec.Components.Data.DataConnectHub.ManagementState = dch
					ctx := &modules.DSCContext{DSC: dsc}
					pm := &configApi.PlatformModules{}
					h := data.NewHandler()
					h.PopulatePlatformModule(pm, ctx)
					wantEnabled := fs == operatorv1.Managed || registry == operatorv1.Managed || dch == operatorv1.Managed
					require.Equal(t, wantEnabled, h.IsEnabled(pm))
					require.Equal(t, "data", h.GetName())
					cli := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
					obj, err := h.BuildModuleCR(context.Background(), cli, ctx, nil)
					require.NoError(t, err)
					require.Equal(t, componentApi.FeastOperatorKind, obj.GetKind())
					require.Equal(t, componentApi.FeastOperatorInstanceName, obj.GetName())
					for name, want := range map[string]operatorv1.ManagementState{
						"featureStore": fs, "dataRegistry": registry, "dataConnectHub": dch,
					} {
						state, found, err := unstructured.NestedString(obj.Object, "spec", "capabilities", name, "managementState")
						require.NoError(t, err)
						require.True(t, found)
						require.Equal(t, string(want), state)
					}
				})
			}
		}
	}
}

func TestDataConnectHubConfigurationProjection(t *testing.T) {
	for _, overrides := range []bool{false, true} {
		t.Run(fmt.Sprintf("overrides=%t", overrides), func(t *testing.T) {
			dsc := &dscApi.DataScienceCluster{}
			config := &dsc.Spec.Components.Data.DataConnectHub
			config.ManagementState = operatorv1.Managed
			if overrides {
				config.Namespace = "custom-dch-infra"
				config.Gateway = &componentApi.DataConnectHubGateway{Name: "custom-gateway", Namespace: "custom-ingress"}
				config.TokenReviewAudiences = []string{"https://override.example"}
			}
			cli := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(&configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       configv1.AuthenticationSpec{ServiceAccountIssuer: "https://discovered.example"},
			}).Build()
			obj, err := data.NewHandler().BuildModuleCR(context.Background(), cli, &modules.DSCContext{DSC: dsc}, nil)
			require.NoError(t, err)
			require.NotPanics(t, func() { _ = obj.DeepCopy() }, "module CR must contain valid unstructured JSON values")
			namespace, _, err := unstructured.NestedString(obj.Object, "spec", "capabilities", "dataConnectHub", "namespace")
			require.NoError(t, err)
			wantNamespace := componentApi.DefaultDataConnectHubNamespace
			if overrides {
				wantNamespace = "custom-dch-infra"
			}
			require.Equal(t, wantNamespace, namespace)
			audiences, _, err := unstructured.NestedStringSlice(obj.Object, "spec", "capabilities", "dataConnectHub", "tokenReviewAudiences")
			require.NoError(t, err)
			if overrides {
				require.Equal(t, []string{"https://override.example"}, audiences)
				gw, _, err := unstructured.NestedString(obj.Object, "spec", "capabilities", "dataConnectHub", "gateway", "name")
				require.NoError(t, err)
				require.Equal(t, "custom-gateway", gw)
			} else {
				require.Equal(t, []string{"https://discovered.example"}, audiences)
			}
		})
	}
}

func TestDataConnectHubOmissionMeansRemoved(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := data.NewHandler()
	obj, err := h.BuildModuleCR(context.Background(), cli, &modules.DSCContext{DSC: &dscApi.DataScienceCluster{}}, nil)
	require.NoError(t, err)
	state, _, err := unstructured.NestedString(obj.Object, "spec", "capabilities", "dataConnectHub", "managementState")
	require.NoError(t, err)
	require.Equal(t, string(operatorv1.Removed), state)
	for _, name := range []string{
		"RELATED_IMAGE_ODH_DATA_CONNECT_HUB_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_DATA_CONNECT_HUB_REST_IMAGE",
		"RELATED_IMAGE_ODH_DATA_CONNECT_HUB_FLIGHT_IMAGE",
	} {
		require.Contains(t, h.GetRelatedImages(), name)
	}
}
