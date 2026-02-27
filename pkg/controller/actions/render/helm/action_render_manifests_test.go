package helm_test

import (
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestRenderHelmChartActionWithLabelsAndAnnotations(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()
	chartDir := filepath.Join("testdata", "test-chart")

	action := helm.NewAction(
		helm.WithCache(false),
		helm.WithLabel("component.opendatahub.io/name", "test-component"),
		helm.WithLabel("platform.opendatahub.io/namespace", ns),
		helm.WithAnnotation("platform.opendatahub.io/release", "1.2.3"),
		helm.WithAnnotation("platform.opendatahub.io/type", "managed"),
	)

	render.RenderedResourcesTotal.Reset()

	// run the renderer in a loop to ensure the cache is off, and the
	// manifests are re-rendered on each iteration
	for i := 1; i < 3; i++ {
		rr := types.ReconciliationRequest{
			Instance: &ccmv1alpha1.AzureKubernetesEngine{},
			HelmCharts: []types.HelmChartInfo{{
				Source: helmRenderer.Source{
					Chart:       chartDir,
					ReleaseName: "test-release",
					Values: helmRenderer.Values(map[string]any{
						"replicaCount": 3,
						"namespace":    ns,
					}),
				},
			}},
		}

		err := action(ctx, &rr)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Generated).Should(BeTrue())
		g.Expect(rr.Resources).Should(And(
			HaveLen(1),
			HaveEach(And(
				jq.Match(`.metadata.namespace == "%s"`, ns),
				jq.Match(`.metadata.labels["component.opendatahub.io/name"] == "%s"`, "test-component"),
				jq.Match(`.metadata.labels["platform.opendatahub.io/namespace"] == "%s"`, ns),
				jq.Match(`.metadata.annotations["platform.opendatahub.io/release"] == "%s"`, "1.2.3"),
				jq.Match(`.metadata.annotations["platform.opendatahub.io/type"] == "%s"`, "managed"),
			)),
		))

		rc := testutil.ToFloat64(render.RenderedResourcesTotal)
		g.Expect(rc).Should(BeNumerically("==", 1*i))
	}
}

func TestRenderHelmChartWithCacheAction(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()
	chartDir := filepath.Join("testdata", "test-chart")

	action := helm.NewAction()

	render.RenderedResourcesTotal.Reset()

	for i := range 3 {
		d := ccmv1alpha1.AzureKubernetesEngine{}

		if i >= 1 {
			d.Generation = 1
		}

		rr := types.ReconciliationRequest{
			Instance: &d,
			HelmCharts: []types.HelmChartInfo{{
				Source: helmRenderer.Source{
					Chart:       chartDir,
					ReleaseName: "test-release",
					Values: helmRenderer.Values(map[string]any{
						"replicaCount": 2,
						"namespace":    ns,
					}),
				},
			}},
		}

		err := action(ctx, &rr)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(And(
			HaveLen(1),
			HaveEach(And(
				jq.Match(`.metadata.namespace == "%s"`, ns),
			)),
		))

		rc := testutil.ToFloat64(render.RenderedResourcesTotal)

		switch i {
		case 0:
			g.Expect(rc).Should(BeNumerically("==", 1))
			g.Expect(rr.Generated).Should(BeTrue())
		case 1:
			g.Expect(rc).Should(BeNumerically("==", 2))
			g.Expect(rr.Generated).Should(BeTrue())
		case 2:
			g.Expect(rc).Should(BeNumerically("==", 2))
			g.Expect(rr.Generated).Should(BeFalse())
		}
	}
}

func TestRenderMultipleHelmCharts(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns1 := xid.New().String()
	ns2 := xid.New().String()
	chartDir := filepath.Join("testdata", "test-chart")

	action := helm.NewAction(
		helm.WithCache(false),
		helm.WithLabel("app", "multi-chart"),
	)

	rr := types.ReconciliationRequest{
		Instance: &ccmv1alpha1.AzureKubernetesEngine{},
		HelmCharts: []types.HelmChartInfo{
			{
				Source: helmRenderer.Source{
					Chart:       chartDir,
					ReleaseName: "release-one",
					Values: helmRenderer.Values(map[string]any{
						"namespace": ns1,
					}),
				},
			},
			{
				Source: helmRenderer.Source{
					Chart:       chartDir,
					ReleaseName: "release-two",
					Values: helmRenderer.Values(map[string]any{
						"namespace": ns2,
					}),
				},
			},
		},
	}

	err := action(ctx, &rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(2))

	g.Expect(rr.Resources[0]).Should(And(
		jq.Match(`.metadata.namespace == "%s"`, ns1),
	))

	g.Expect(rr.Resources[1]).Should(And(
		jq.Match(`.metadata.namespace == "%s"`, ns2),
	))
}

func TestCRDAndCRRender(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()
	chartDir := filepath.Join("testdata", "with-crd")

	action := helm.NewAction(
		helm.WithCache(false),
	)

	rr := types.ReconciliationRequest{
		Instance: &ccmv1alpha1.AzureKubernetesEngine{},
		HelmCharts: []types.HelmChartInfo{{
			Source: helmRenderer.Source{
				Chart:       chartDir,
				ReleaseName: "test-crd-release",
				Values: helmRenderer.Values(map[string]any{
					"namespace": ns,
				}),
			},
		}},
	}

	err := action(ctx, &rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(HaveLen(2))

	// Verify the CRD is rendered first, then the CR
	// This ordering is important for proper installation
	g.Expect(rr.Resources[0]).Should(
		jq.Match(`.kind == "CustomResourceDefinition"`),
	)
	g.Expect(rr.Resources[1]).Should(And(
		jq.Match(`.kind == "TestResource"`),
		jq.Match(`.metadata.namespace == "%s"`, ns),
		jq.Match(`.metadata.name == "test-crd-release-instance"`),
	))
}
