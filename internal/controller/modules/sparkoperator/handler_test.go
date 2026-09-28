package sparkoperator_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/sparkoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

const testAppsNS = "opendatahub"

func newDSCContext(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
	}
	dsc.Spec.Components.SparkOperator.ManagementState = mgmtState

	return &modules.DSCContext{DSC: dsc}
}

func newModuleCRConfig() *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		ApplicationsNamespace: testAppsNS,
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	pm := &configv1alpha1.PlatformModules{
		SparkOperator: common.ManagementSpec{ManagementState: operatorv1.Managed},
	}
	g.Expect(h.IsEnabled(pm)).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	pm := &configv1alpha1.PlatformModules{
		SparkOperator: common.ManagementSpec{ManagementState: operatorv1.Removed},
	}
	g.Expect(h.IsEnabled(pm)).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	pm := &configv1alpha1.PlatformModules{
		SparkOperator: common.ManagementSpec{ManagementState: ""},
	}
	g.Expect(h.IsEnabled(pm)).Should(BeFalse())
}

func TestIsEnabled_Nil(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestPopulatePlatformModule(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()

	pm := &configv1alpha1.PlatformModules{}
	h.PopulatePlatformModule(pm, newDSCContext(operatorv1.Managed))

	g.Expect(pm.SparkOperator.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_NilSafe(t *testing.T) {
	h := sparkoperator.NewHandler()

	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configv1alpha1.PlatformModules{}, nil)
	h.PopulatePlatformModule(&configv1alpha1.PlatformModules{}, &modules.DSCContext{})
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()

	u, err := h.BuildModuleCR(context.Background(), nil, newDSCContext(operatorv1.Managed), newModuleCRConfig())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.SparkOperatorInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.SparkOperatorKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"managementState is a DSC-level field and must not be projected into the component CR")
}

func TestBuildModuleCR_NilDSCContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, newModuleCRConfig())
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{}, newModuleCRConfig())
	g.Expect(err).Should(HaveOccurred())
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.SparkOperatorComponentName))
}

func TestGetDeploymentName(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()
	g.Expect(h.GetDeploymentName()).Should(Equal("spark-operator-module-controller-manager"))
}

func TestImageHandling(t *testing.T) {
	g := NewWithT(t)
	h := sparkoperator.NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_SPARK_OPERATOR_MODULE_IMAGE"))

	g.Expect(h.GetRelatedImages()).Should(ConsistOf(
		"RELATED_IMAGE_ODH_SPARK_OPERATOR_IMAGE",
	))

	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_SPARK_OPERATOR_MODULE_IMAGE"))
}

func TestGetOperatorManifests(t *testing.T) {
	h := sparkoperator.NewHandler()

	cases := []struct {
		name     string
		platform common.Platform
		want     string
	}{
		{"odh", cluster.OpenDataHub, "/base/sparkoperator/default"},
		{"self-managed-rhoai", cluster.SelfManagedRhoai, "/base/sparkoperator/default"},
		{"managed-rhoai", cluster.ManagedRhoai, "/base/sparkoperator/default"},
		{"xks", cluster.XKS, "/base/sparkoperator/default"},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &modules.PlatformContext{
				ApplicationsNamespace: testAppsNS,
				ManifestsBasePath:     "/base",
				Release:               common.Release{Name: tcase.platform},
			}

			m := h.GetOperatorManifests(ctx)
			g.Expect(m.HelmCharts).Should(BeEmpty())
			g.Expect(m.Manifests).Should(HaveLen(1))
			g.Expect(m.Manifests[0].String()).Should(Equal(tcase.want))
		})
	}
}
