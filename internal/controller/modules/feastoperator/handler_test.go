package feastoperator_test

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
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/feastoperator"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configv1alpha1.PlatformModules {
	return &configv1alpha1.PlatformModules{
		FeastOperator: common.ManagementSpec{
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
	h := feastoperator.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_EmptyModules(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	g.Expect(h.IsEnabled(&configv1alpha1.PlatformModules{})).Should(BeFalse())
}

func TestBuildModuleCR_NilClientReturnsError_BothNil(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilClientReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()

	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("kubernetes client is nil"))
}

func TestBuildModuleCR_NonOIDCCluster(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()

	cli := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

	u, err := h.BuildModuleCR(context.Background(), cli, nil, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.FeastOperatorInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.FeastOperatorKind))
	g.Expect(u.GetAPIVersion()).Should(Equal("components.platform.opendatahub.io/v1alpha1"))
}

func TestBuildModuleCR_OIDCIssuerProjected(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()

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
	h := feastoperator.NewHandler()

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
	h := feastoperator.NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_FEAST_MODULE_OPERATOR_IMAGE"))

	g.Expect(h.GetRelatedImages()).Should(ConsistOf(
		"RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE",
	))

	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_FEAST_MODULE_OPERATOR_IMAGE"))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.FeastOperatorComponentName))
}

func TestGetGVK(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	gvk := h.GetGVK()
	g.Expect(gvk.Group).Should(Equal("components.platform.opendatahub.io"))
	g.Expect(gvk.Version).Should(Equal("v1alpha1"))
	g.Expect(gvk.Kind).Should(Equal("FeastOperator"))
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
