package databaseservice_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/databaseservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

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

func TestGetOperatorManifests_SCCValuesByPlatform(t *testing.T) {
	g := NewWithT(t)
	h := databaseservice.NewHandler()

	getValues := func(t *testing.T, release common.Platform) map[string]any {
		t.Helper()
		m := h.GetOperatorManifests(&modules.PlatformContext{
			ApplicationsNamespace: testAppsNS,
			ChartsBasePath:        "/opt/charts",
			Release:               common.Release{Name: release},
		})
		g.Expect(m.HelmCharts).Should(HaveLen(1))
		vals, err := m.HelmCharts[0].Values(t.Context())
		g.Expect(err).ShouldNot(HaveOccurred())
		return vals
	}

	t.Run("OpenShift platforms force-include the Internal Postgres SCC", func(t *testing.T) {
		for _, release := range []common.Platform{cluster.OpenDataHub, cluster.SelfManagedRhoai, cluster.ManagedRhoai} {
			vals := getValues(t, release)
			g.Expect(vals).Should(HaveKey("openshift"))
			g.Expect(vals["openshift"]).Should(HaveKeyWithValue("securityContextConstraints", true))
		}
	})

	t.Run("XKS omits the SCC gate", func(t *testing.T) {
		vals := getValues(t, cluster.XKS)
		g.Expect(vals).ShouldNot(HaveKey("openshift"))
	})

	t.Run("Empty or unknown platforms fail closed", func(t *testing.T) {
		for _, release := range []common.Platform{"", "unknown-platform"} {
			vals := getValues(t, release)
			g.Expect(vals).ShouldNot(HaveKey("openshift"))
		}
	})
}

func TestDeleteOperatorResources_RemovesSCC(t *testing.T) {
	g := NewWithT(t)

	chartsRoot, err := envtestutil.FindProjectRoot()
	if err != nil {
		t.Skipf("failed to resolve project root: %v", err)
	}
	chartsRoot = filepath.Join(chartsRoot, "opt", "charts")
	if _, err := os.Stat(filepath.Join(chartsRoot, "databaseservice")); err != nil {
		t.Skipf("databaseservice chart not found under %s (run make get-manifests first)", chartsRoot)
	}

	newPlatform := func(release common.Platform) *modules.PlatformContext {
		return &modules.PlatformContext{
			ApplicationsNamespace: testAppsNS,
			ChartsBasePath:        chartsRoot,
			Release:               common.Release{Name: release},
		}
	}

	seedCluster := func(t *testing.T) client.Client {
		t.Helper()
		scc := &unstructured.Unstructured{}
		scc.SetGroupVersionKind(gvk.SecurityContextConstraints)
		scc.SetName("odh-db-internal-postgres")

		role := &unstructured.Unstructured{}
		role.SetGroupVersionKind(gvk.ClusterRole)
		role.SetName("odh-db-internal-postgres")

		cli, err := fakeclient.New(
			fakeclient.WithObjects(scc, role),
			fakeclient.WithGVKs(
				fakeclient.GVKMapping{GVK: gvk.SecurityContextConstraints, Scope: meta.RESTScopeRoot},
				fakeclient.GVKMapping{GVK: gvk.ClusterRole, Scope: meta.RESTScopeRoot},
			),
		)
		if err != nil {
			t.Fatalf("create fake client: %v", err)
		}
		return cli
	}

	assertSCCExists := func(t *testing.T, cli client.Client, wantExists bool) {
		t.Helper()
		scc := &unstructured.Unstructured{}
		scc.SetGroupVersionKind(gvk.SecurityContextConstraints)
		err := cli.Get(t.Context(), client.ObjectKey{Name: "odh-db-internal-postgres"}, scc)
		if wantExists {
			g.Expect(err).ShouldNot(HaveOccurred(), "SCC should still exist")
		} else {
			g.Expect(k8serr.IsNotFound(err)).Should(BeTrue(), "SCC should be deleted, got: %v", err)
		}
	}

	t.Run("OpenShift platform deletes the SCC and ClusterRole", func(t *testing.T) {
		h := databaseservice.NewHandler()
		cli := seedCluster(t)

		err := h.DeleteOperatorResources(t.Context(), cli, newPlatform(cluster.OpenDataHub))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCExists(t, cli, false)

		role := &unstructured.Unstructured{}
		role.SetGroupVersionKind(gvk.ClusterRole)
		g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: "odh-db-internal-postgres"}, role).Error()).
			Should(ContainSubstring("not found"), "ClusterRole should be deleted")
	})

	t.Run("XKS platform leaves the SCC untouched", func(t *testing.T) {
		h := databaseservice.NewHandler()
		cli := seedCluster(t)

		// Install on XKS never rendered the SCC, so cleanup must not delete
		// a same-named object it does not own.
		err := h.DeleteOperatorResources(t.Context(), cli, newPlatform(cluster.XKS))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCExists(t, cli, true)
	})
}
