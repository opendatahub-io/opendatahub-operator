package databaseservice_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	operatorv1 "github.com/openshift/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/databaseservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestGetOperatorManifests_LegacyChartSchema(t *testing.T) {
	for _, release := range []common.Platform{cluster.OpenDataHub, cluster.SelfManagedRhoai, cluster.ManagedRhoai, cluster.XKS} {
		t.Run(string(release), func(t *testing.T) {
			g := NewWithT(t)
			h := databaseservice.NewHandler()
			manifests := h.GetOperatorManifests(&modules.PlatformContext{
				ApplicationsNamespace: testAppsNS,
				ChartsBasePath:        filepath.Join("testdata", "rhoaieng-102095"),
				Release:               common.Release{Name: release},
			})
			g.Expect(manifests.HelmCharts).Should(HaveLen(1))
			renderer, err := helmRenderer.New([]helmRenderer.Source{manifests.HelmCharts[0].Source})
			g.Expect(err).ShouldNot(HaveOccurred())
			resources, err := renderer.Process(t.Context(), nil)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(resources).Should(HaveLen(1))
			g.Expect(resources[0].GetName()).Should(Equal("databaseservice-schema-repro"))
			g.Expect(resources[0].GetNamespace()).Should(Equal(testAppsNS))
		})
	}
}

func TestGetOperatorManifests_ChartValuesErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		values string
	}{
		{name: "missing values file"},
		{name: "invalid values YAML", values: "openshift: ["},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			chartsRoot := t.TempDir()
			chartPath := filepath.Join(chartsRoot, "databaseservice")
			g.Expect(os.MkdirAll(chartPath, 0o755)).Should(Succeed())
			if tt.values != "" {
				g.Expect(os.WriteFile(filepath.Join(chartPath, "values.yaml"), []byte(tt.values), 0o600)).Should(Succeed())
			}
			h := databaseservice.NewHandler()
			manifests := h.GetOperatorManifests(&modules.PlatformContext{
				ChartsBasePath: chartsRoot,
				Release:        common.Release{Name: cluster.OpenDataHub},
			})
			_, err := manifests.HelmCharts[0].Values(t.Context())
			g.Expect(err).Should(HaveOccurred(), "chart errors must not silently disable SCC rendering")
		})
	}
}

// TestDeleteOperatorResources_LegacyChartSchema reproduces RHOAIENG-102095:
// cleanup of a Removed module must not block platform startup on a schema error.
func TestDeleteOperatorResources_LegacyChartSchema(t *testing.T) {
	h := databaseservice.NewHandler()

	for _, tt := range []struct {
		name    string
		cleanup func(context.Context, client.Client, *modules.PlatformContext) error
	}{
		{
			name:    "historical chart without the SCC value renders",
			cleanup: h.BaseHandler.DeleteOperatorResources,
		},
		{
			name:    "Removed module cleanup accepts the historical chart",
			cleanup: h.DeleteOperatorResources,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			platform := &modules.PlatformContext{
				ApplicationsNamespace: testAppsNS,
				ChartsBasePath:        filepath.Join("testdata", "rhoaieng-102095"),
				Release:               common.Release{Name: cluster.SelfManagedRhoai},
				Modules: &configApi.PlatformModules{
					DatabaseService: common.ManagementSpec{ManagementState: operatorv1.Removed},
				},
			}
			g.Expect(h.IsEnabled(platform.Modules)).Should(BeFalse())

			var deleted []string
			cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					deleted = append(deleted, obj.GetName())
					return nil
				},
			}))
			g.Expect(err).ShouldNot(HaveOccurred())

			err = tt.cleanup(t.Context(), cli, platform)
			g.Expect(err).ShouldNot(HaveOccurred(), "cleanup of a Removed module must not block platform startup")
			g.Expect(deleted).Should(ConsistOf("databaseservice-schema-repro"))
		})
	}
}
