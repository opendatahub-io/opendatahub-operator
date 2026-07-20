package monitoring_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					Monitoring: common.ManagementSpec{
						ManagementState: mgmtState,
					},
				},
			},
		},
	}
}

func newDSCI(mgmtState operatorv1.ManagementState) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "opendatahub",
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: mgmtState,
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: "opendatahub",
				},
			},
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilPlatform(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	ctx := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestBuildModuleCR_NilDSCIReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	dsci := newDSCI(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSCI: dsci})
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(serviceApi.MonitoringInstanceName))
	g.Expect(u.GetKind()).Should(Equal(serviceApi.MonitoringKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"managementState is a DSCI-level field and must not be projected into the module CR")
	g.Expect(spec["namespace"]).Should(Equal("opendatahub"))
}

func TestBuildModuleCR_ProjectsMetrics(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Storage: &serviceApi.MetricsStorage{
			Size:      resource.MustParse("10Gi"),
			Retention: "7d",
		},
		Replicas: 2,
	}

	u, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSCI: dsci})
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	metrics, ok := spec["metrics"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.metrics missing")

	storage, ok := metrics["storage"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.metrics.storage missing")
	g.Expect(storage["size"]).Should(Equal("10Gi"))
	g.Expect(storage["retention"]).Should(Equal("7d"))
	g.Expect(metrics["replicas"]).Should(Equal(int64(2)))
}

func TestBuildModuleCR_ProjectsTraces(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Traces = &serviceApi.Traces{
		Storage: serviceApi.TracesStorage{
			Backend: serviceApi.StorageBackendS3,
			Secret:  "my-s3-creds",
			Retention: metav1.Duration{
				Duration: 3600000000000, // 1h
			},
		},
		SampleRatio: "0.5",
		TLS: &serviceApi.TracesTLS{
			Enabled:           true,
			CertificateSecret: "tls-secret",
			CAConfigMap:       "ca-cm",
		},
	}

	u, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSCI: dsci})
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	traces, ok := spec["traces"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.traces missing")

	storage, ok := traces["storage"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.traces.storage missing")
	g.Expect(storage["backend"]).Should(Equal("s3"))
	g.Expect(storage["secret"]).Should(Equal("my-s3-creds"))
	g.Expect(storage["retention"]).Should(Equal("1h0m0s"))

	g.Expect(traces["sampleRatio"]).Should(Equal("0.5"))

	tls, ok := traces["tls"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.traces.tls missing")
	g.Expect(tls["enabled"]).Should(BeTrue())
	g.Expect(tls["certificateSecret"]).Should(Equal("tls-secret"))
	g.Expect(tls["caConfigMap"]).Should(Equal("ca-cm"))
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ConsistOf(
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
		"RELATED_IMAGE_OSE_PROM_LABEL_PROXY_IMAGE",
		"RELATED_IMAGE_PERSES_IMAGE",
	))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.GetName()).Should(Equal(serviceApi.MonitoringServiceName))
}
