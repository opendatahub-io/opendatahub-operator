package data_test

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/data"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
		Data: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = configv1.Install(scheme)
	_ = serviceApi.AddToScheme(scheme)
	return scheme
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_EmptyModules(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	g.Expect(h.IsEnabled(&configApi.PlatformModules{})).Should(BeFalse())
}

func TestBuildModuleCR_NilClientReturnsError_BothNil(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilClientReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()

	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("kubernetes client is nil"))
}

func TestBuildModuleCR_NonOIDCCluster(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()

	cli := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

	u, err := h.BuildModuleCR(context.Background(), cli, nil, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.FeastOperatorInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.FeastOperatorKind))
	g.Expect(u.GetAPIVersion()).Should(Equal("components.platform.opendatahub.io/v1alpha1"))
	spec, ok := unstructuredNestedMap(u.Object, "spec")
	g.Expect(ok).To(BeTrue())
	g.Expect(spec).NotTo(HaveKey("capabilities"))
}

func TestBuildModuleCR_CapabilitiesFromDSC(t *testing.T) {
	tests := []struct {
		name              string
		featureStoreState operatorv1.ManagementState
		dataRegistryState operatorv1.ManagementState
		wantFeatureStore  string
		wantDataRegistry  string
	}{
		{name: "feature store only", featureStoreState: operatorv1.Managed, dataRegistryState: operatorv1.Removed, wantFeatureStore: "Managed", wantDataRegistry: "Removed"},
		{name: "data registry only", featureStoreState: operatorv1.Removed, dataRegistryState: operatorv1.Managed, wantFeatureStore: "Removed", wantDataRegistry: "Managed"},
		{name: "both managed", featureStoreState: operatorv1.Managed, dataRegistryState: operatorv1.Managed, wantFeatureStore: "Managed", wantDataRegistry: "Managed"},
		{name: "unset states default to removed", wantFeatureStore: "Removed", wantDataRegistry: "Removed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := &dscApi.DataScienceCluster{}
			dsc.Spec.Components.Data.FeatureStore.ManagementState = tt.featureStoreState
			dsc.Spec.Components.Data.DataRegistry.ManagementState = tt.dataRegistryState
			cli := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

			u, err := feastoperator.NewHandler().BuildModuleCR(
				context.Background(), cli, &modules.DSCContext{DSC: dsc}, nil,
			)
			g.Expect(err).ShouldNot(HaveOccurred())

			featureStore, ok := unstructuredNestedMap(u.Object, "spec", "capabilities", "featureStore")
			g.Expect(ok).To(BeTrue())
			g.Expect(featureStore["managementState"]).To(Equal(tt.wantFeatureStore))

			dataRegistry, ok := unstructuredNestedMap(u.Object, "spec", "capabilities", "dataRegistry")
			g.Expect(ok).To(BeTrue())
			g.Expect(dataRegistry["managementState"]).To(Equal(tt.wantDataRegistry))
		})
	}
}

func TestBuildModuleCR_OIDCIssuerProjected(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()

	cli := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(
			&configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       configv1.AuthenticationSpec{Type: "OIDC"},
			},
			&serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					OIDC: &serviceApi.OIDCConfig{
						IssuerURL: "https://keycloak.example.com/realms/odh",
					},
				},
			},
		).
		Build()

	u, err := h.BuildModuleCR(context.Background(), cli, nil, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := unstructuredNestedMap(u.Object, "spec")
	oidc, ok := spec["oidc"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "spec.oidc should exist")
	g.Expect(oidc["issuerURL"]).To(Equal("https://keycloak.example.com/realms/odh"))
}

func TestBuildModuleCR_InvalidIssuerReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()

	cli := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(
			&configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       configv1.AuthenticationSpec{Type: "OIDC"},
			},
			&serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					OIDC: &serviceApi.OIDCConfig{
						IssuerURL: "http://not-https.example.com",
					},
				},
			},
		).
		Build()

	_, err := h.BuildModuleCR(context.Background(), cli, nil, nil)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("https"))
}

func TestImageHandling(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_FEAST_MODULE_OPERATOR_IMAGE"))

	g.Expect(h.GetRelatedImages()).Should(ConsistOf(
		"RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE",
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
	))

	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_FEAST_MODULE_OPERATOR_IMAGE"))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.DataModuleName))
}

func TestGetGVK(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	gvk := h.GetGVK()
	g.Expect(gvk.Group).Should(Equal("components.platform.opendatahub.io"))
	g.Expect(gvk.Version).Should(Equal("v1alpha1"))
	g.Expect(gvk.Kind).Should(Equal("FeastOperator"))
}

func TestGetReadyConditionType(t *testing.T) {
	g := NewWithT(t)
	h := data.NewHandler()
	g.Expect(h.GetReadyConditionType()).Should(Equal("DataReady"))
}

func TestWriteDSCComponentStatus(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled bool
		want    operatorv1.ManagementState
	}{
		{name: "managed", enabled: true, want: operatorv1.Managed},
		{name: "removed", enabled: false, want: operatorv1.Removed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := &dscApi.DataScienceCluster{}

			data.NewHandler().WriteDSCComponentStatus(dsc, tt.enabled, nil)

			g.Expect(dsc.Status.Components.Data.ManagementState).Should(Equal(tt.want))
		})
	}
}

func unstructuredNestedMap(obj map[string]any, fields ...string) (map[string]any, bool) {
	val := obj
	for _, f := range fields {
		next, ok := val[f]
		if !ok {
			return nil, false
		}
		m, ok := next.(map[string]any)
		if !ok {
			return nil, false
		}
		val = m
	}
	return val, true
}
