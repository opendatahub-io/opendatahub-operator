package databaseservice_test

import (
	"context"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/databaseservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

const testAppsNS = "opendatahub"

func newDSCContext(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	dsc := &dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
	}
	dsc.Spec.Components.DatabaseService.ManagementState = mgmtState

	return &modules.DSCContext{DSC: dsc}
}

func newModuleCRConfig() *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		ApplicationsNamespace: testAppsNS,
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	pm := &configApi.PlatformModules{
		DatabaseService: common.ManagementSpec{ManagementState: operatorv1.Managed},
	}
	g.Expect(h.IsEnabled(pm)).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	pm := &configApi.PlatformModules{
		DatabaseService: common.ManagementSpec{ManagementState: operatorv1.Removed},
	}
	g.Expect(h.IsEnabled(pm)).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	pm := &configApi.PlatformModules{
		DatabaseService: common.ManagementSpec{ManagementState: ""},
	}
	g.Expect(h.IsEnabled(pm)).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestPopulatePlatformModule_Managed(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	pm := &configApi.PlatformModules{}

	h.PopulatePlatformModule(pm, newDSCContext(operatorv1.Managed))
	g.Expect(pm.DatabaseService.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_EmptyDefaultsToRemoved(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	pm := &configApi.PlatformModules{}

	h.PopulatePlatformModule(pm, newDSCContext(""))
	g.Expect(pm.DatabaseService.ManagementState).Should(Equal(operatorv1.Removed))
}

func TestPopulatePlatformModule_NilSafe(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configApi.PlatformModules{}, nil)
	h.PopulatePlatformModule(&configApi.PlatformModules{}, &modules.DSCContext{})
	g.Expect(true).Should(BeTrue())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()

	u, err := h.BuildModuleCR(context.Background(), nil, newDSCContext(operatorv1.Managed), newModuleCRConfig())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.DatabaseServiceInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.DatabaseServiceKind))
	g.Expect(u.GroupVersionKind()).Should(Equal(gvk.DatabaseService))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).Should(BeEmpty())
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"managementState is a DSC-level field and must not be projected into the module CR")
}

func TestBuildModuleCR_NilDSCContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, newModuleCRConfig())
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{}, newModuleCRConfig())
	g.Expect(err).Should(HaveOccurred())
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.DatabaseServiceComponentName))
}

func TestGetDeploymentName(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	g.Expect(h.GetDeploymentName()).Should(Equal("odh-db-operator-operator"))
}

func TestImageHandling(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE"))
	g.Expect(h.GetRelatedImages()).Should(BeEmpty())
}

func TestGetOperatorManifests(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()
	platform := &modules.PlatformContext{
		ApplicationsNamespace: testAppsNS,
		ChartsBasePath:        "/opt/charts",
	}

	m := h.GetOperatorManifests(platform)
	g.Expect(m.HelmCharts).Should(HaveLen(1))
	g.Expect(m.HelmCharts[0].Chart).Should(Equal(filepath.Join("/opt/charts", "databaseservice")))
	// Chart templates use .Release.Namespace; empty release namespace makes
	// disabled-module cleanup delete Role/ServiceAccount/etc. with namespace="".
	g.Expect(m.HelmCharts[0].ReleaseNamespace).Should(Equal(testAppsNS))
	g.Expect(m.Manifests).Should(BeEmpty())
}
