package databaseservice_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

const (
	testPlatformUID = "11111111-2222-3333-4444-555555555555"
	testSCCName     = "odh-db-internal-postgres"
)

// sccSeed configures the cluster objects seeded for DeleteOperatorResources
// ownership tests.
type sccSeed struct {
	ownedLabel   bool
	ownerRefUID  string // ownerReference UID on the SCC/ClusterRole; "" = none
	withPlatform bool   // seed the Platform CR
	platformUID  string
}

func requireDatabaseserviceChartsRoot(t *testing.T) string {
	t.Helper()

	root, err := envtestutil.FindProjectRoot()
	if err != nil {
		t.Skipf("failed to resolve project root: %v", err)
	}
	root = filepath.Join(root, "opt", "charts")
	if _, err := os.Stat(filepath.Join(root, "databaseservice")); err != nil {
		t.Skipf("databaseservice chart not found under %s (run make get-manifests first)", root)
	}
	return root
}

func newDeleteTestPlatform(t *testing.T, release common.Platform) *modules.PlatformContext {
	t.Helper()

	return &modules.PlatformContext{
		ApplicationsNamespace: testAppsNS,
		ChartsBasePath:        requireDatabaseserviceChartsRoot(t),
		Release:               common.Release{Name: release},
	}
}

func seedSCCCluster(t *testing.T, seed sccSeed) client.Client {
	t.Helper()

	ownedLabels := map[string]string{}
	if seed.ownedLabel {
		ownedLabels = map[string]string{
			"db.infrastructure.opendatahub.io/internal-postgres-scc": "owned",
		}
	}

	newSeeded := func(gvkToUse schema.GroupVersionKind) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvkToUse)
		obj.SetName("odh-db-internal-postgres")
		obj.SetLabels(ownedLabels)
		if seed.ownerRefUID != "" {
			obj.SetOwnerReferences([]metav1.OwnerReference{{
				APIVersion: configApi.GroupVersion.String(),
				Kind:       "Platform",
				Name:       configApi.PlatformInstanceName,
				UID:        k8stypes.UID(seed.ownerRefUID),
			}})
		}
		return obj
	}

	objs := []client.Object{
		newSeeded(gvk.SecurityContextConstraints),
		newSeeded(gvk.ClusterRole),
	}
	if seed.withPlatform {
		objs = append(objs, &configApi.Platform{
			ObjectMeta: metav1.ObjectMeta{
				Name: configApi.PlatformInstanceName,
				UID:  k8stypes.UID(seed.platformUID),
			},
		})
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(objs...),
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

func assertSCCResourceExists(t *testing.T, cli client.Client, gvkToCheck schema.GroupVersionKind, wantExists bool) {
	t.Helper()
	g := NewWithT(t)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvkToCheck)
	err := cli.Get(t.Context(), client.ObjectKey{Name: testSCCName}, obj)
	if wantExists {
		g.Expect(err).ShouldNot(HaveOccurred(), "%s should still exist", testSCCName)
	} else {
		g.Expect(k8serr.IsNotFound(err)).Should(BeTrue(), "%s should be deleted, got: %v", testSCCName, err)
	}
}

func TestDeleteOperatorResources_RemovesSCC(t *testing.T) {
	t.Run("OpenShift platform deletes the owned SCC and ClusterRole", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()
		cli := seedSCCCluster(t, sccSeed{
			ownedLabel:   true,
			ownerRefUID:  testPlatformUID,
			withPlatform: true,
			platformUID:  testPlatformUID,
		})

		err := h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.OpenDataHub))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCResourceExists(t, cli, gvk.SecurityContextConstraints, false)
		assertSCCResourceExists(t, cli, gvk.ClusterRole, false)
	})

	t.Run("standalone install without Platform ownerReference is left untouched", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()
		cli := seedSCCCluster(t, sccSeed{
			// Same chart, same ownership label, but applied by a standalone
			// install: no Platform ownerReference.
			ownedLabel:   true,
			ownerRefUID:  "",
			withPlatform: true,
			platformUID:  testPlatformUID,
		})

		err := h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.OpenDataHub))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCResourceExists(t, cli, gvk.SecurityContextConstraints, true)
		assertSCCResourceExists(t, cli, gvk.ClusterRole, true)
	})

	t.Run("stale Platform ownerReference is left untouched", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()
		cli := seedSCCCluster(t, sccSeed{
			// Applied by a previous Platform incarnation (UID differs).
			ownedLabel:   true,
			ownerRefUID:  "stale-uid-from-old-platform",
			withPlatform: true,
			platformUID:  testPlatformUID,
		})

		err := h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.OpenDataHub))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCResourceExists(t, cli, gvk.SecurityContextConstraints, true)
		assertSCCResourceExists(t, cli, gvk.ClusterRole, true)
	})

	t.Run("foreign unlabeled SCC is left untouched", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()
		cli := seedSCCCluster(t, sccSeed{
			// Admin-created object: no label, no ownerReference.
			ownedLabel:   false,
			ownerRefUID:  "",
			withPlatform: true,
			platformUID:  testPlatformUID,
		})

		err := h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.OpenDataHub))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCResourceExists(t, cli, gvk.SecurityContextConstraints, true)
		assertSCCResourceExists(t, cli, gvk.ClusterRole, true)
	})

	t.Run("absent Platform leaves the SCC to garbage collection", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()
		cli := seedSCCCluster(t, sccSeed{
			// Platform CR deleted: K8s GC owns teardown via the controller
			// ownerReference, so cleanup must not act.
			ownedLabel:   true,
			ownerRefUID:  testPlatformUID,
			withPlatform: false,
		})

		err := h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.OpenDataHub))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCResourceExists(t, cli, gvk.SecurityContextConstraints, true)
		assertSCCResourceExists(t, cli, gvk.ClusterRole, true)
	})

	t.Run("XKS platform leaves the SCC untouched", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()
		cli := seedSCCCluster(t, sccSeed{
			ownedLabel:   true,
			ownerRefUID:  testPlatformUID,
			withPlatform: true,
			platformUID:  testPlatformUID,
		})

		// Install on XKS never rendered the SCC, so cleanup must not delete
		// a same-named object it does not own.
		err := h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.XKS))
		g.Expect(err).ShouldNot(HaveOccurred())

		assertSCCResourceExists(t, cli, gvk.SecurityContextConstraints, true)
	})
}

func TestDeleteOperatorResources_OwnershipLookupFailsClosed(t *testing.T) {
	t.Run("ownership lookup failure fails closed", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()

		var deleted []string
		cli, err := fakeclient.New(
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == "odh-db-internal-postgres" {
						// Transient lookup failure: Delete below would succeed.
						return k8serr.NewInternalError(errors.New("transient apiserver error"))
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deleted = append(deleted, obj.GetName())
					return c.Delete(ctx, obj, opts...)
				},
			}),
			fakeclient.WithGVKs(
				fakeclient.GVKMapping{GVK: gvk.SecurityContextConstraints, Scope: meta.RESTScopeRoot},
				fakeclient.GVKMapping{GVK: gvk.ClusterRole, Scope: meta.RESTScopeRoot},
			),
		)
		if err != nil {
			t.Fatalf("create fake client: %v", err)
		}

		err = h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.OpenDataHub))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("checking ownership"))
		// A failed lookup must not authorize deleting the unowned object.
		g.Expect(deleted).ShouldNot(ContainElement("odh-db-internal-postgres"))
	})

	t.Run("Platform lookup failure fails closed", func(t *testing.T) {
		g := NewWithT(t)
		h := databaseservice.NewHandler()

		var deleted []string
		scc := &unstructured.Unstructured{}
		scc.SetGroupVersionKind(gvk.SecurityContextConstraints)
		scc.SetName("odh-db-internal-postgres")
		scc.SetLabels(map[string]string{"db.infrastructure.opendatahub.io/internal-postgres-scc": "owned"})

		cli, err := fakeclient.New(
			fakeclient.WithObjects(
				scc,
				&configApi.Platform{
					ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName, UID: k8stypes.UID(testPlatformUID)},
				},
			),
			fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configApi.Platform); ok {
						// Transient Platform lookup failure: the SCC lookup
						// succeeds and Delete would too.
						return k8serr.NewInternalError(errors.New("transient apiserver error"))
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deleted = append(deleted, obj.GetName())
					return c.Delete(ctx, obj, opts...)
				},
			}),
			fakeclient.WithGVKs(
				fakeclient.GVKMapping{GVK: gvk.SecurityContextConstraints, Scope: meta.RESTScopeRoot},
				fakeclient.GVKMapping{GVK: gvk.ClusterRole, Scope: meta.RESTScopeRoot},
			),
		)
		if err != nil {
			t.Fatalf("create fake client: %v", err)
		}

		err = h.DeleteOperatorResources(t.Context(), cli, newDeleteTestPlatform(t, cluster.OpenDataHub))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("resolving Platform CR"))
		g.Expect(deleted).ShouldNot(ContainElement("odh-db-internal-postgres"))
	})
}
