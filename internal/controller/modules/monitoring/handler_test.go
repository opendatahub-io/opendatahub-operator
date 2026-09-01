package monitoring_test

import (
	"context"
	"path/filepath"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configv1alpha1.PlatformModules {
	return &configv1alpha1.PlatformModules{
		Monitoring: common.ManagementSpec{
			ManagementState: mgmtState,
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

func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = configv1.Install(scheme)
	_ = corev1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func newInfrastructure(topology configv1.TopologyMode) *configv1.Infrastructure {
	return &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology: topology,
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(""))).Should(BeFalse())
}

func TestIsEnabled_EmptyModules(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(&configv1alpha1.PlatformModules{})).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestPopulatePlatformModule_Managed(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	pm := &configv1alpha1.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSCI: newDSCI(operatorv1.Managed)})
	g.Expect(pm.Monitoring.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_EmptyDefaultsToRemoved(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	pm := &configv1alpha1.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSCI: newDSCI("")})
	g.Expect(pm.Monitoring.ManagementState).Should(Equal(operatorv1.Removed))
}

func TestPopulatePlatformModule_NilGuards(t *testing.T) {
	h := monitoring.NewHandler()
	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configv1alpha1.PlatformModules{}, nil)
	h.PopulatePlatformModule(&configv1alpha1.PlatformModules{}, &modules.DSCContext{})
}

func TestBuildModuleCR_NilDSCIReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	dsci := newDSCI(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSCI: dsci}, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(serviceApi.MonitoringInstanceName))
	g.Expect(u.GetKind()).Should(Equal(serviceApi.MonitoringKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"managementState is a DSCI-level field and must not be projected into the module CR")
	g.Expect(spec["namespace"]).Should(Equal("opendatahub"))
	g.Expect(spec).ShouldNot(HaveKey("collectorReplicas"))
	g.Expect(spec).ShouldNot(HaveKey("metrics"))
	g.Expect(spec).ShouldNot(HaveKey("traces"))
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

	u, err := h.BuildModuleCR(context.Background(), newFakeClient(), &modules.DSCContext{DSCI: dsci}, nil)
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

	u, err := h.BuildModuleCR(context.Background(), newFakeClient(), &modules.DSCContext{DSCI: dsci}, nil)
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

func TestBuildModuleCR_TracesWithTLSDisabled(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Traces = &serviceApi.Traces{
		Storage: serviceApi.TracesStorage{
			Backend: serviceApi.StorageBackendPV,
		},
		TLS: &serviceApi.TracesTLS{
			Enabled: false,
		},
	}

	u, err := h.BuildModuleCR(context.Background(), newFakeClient(), &modules.DSCContext{DSCI: dsci}, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	traces, ok := spec["traces"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.traces missing")
	g.Expect(traces).ShouldNot(HaveKey("tls"))
}

func TestBuildModuleCR_MetricsWithoutStorageNulled(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Replicas: 2,
	}

	u, err := h.BuildModuleCR(context.Background(), newFakeClient(), &modules.DSCContext{DSCI: dsci}, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec).ShouldNot(HaveKey("metrics"))
	g.Expect(spec).ShouldNot(HaveKey("collectorReplicas"))
}

func TestBuildModuleCR_MetricsWithExportersWithoutStorage(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	dsci := newDSCI(operatorv1.Managed)
	dsci.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Exporters: map[string]runtime.RawExtension{
			"custom": {Raw: []byte(`{"endpoint":"http://example.com"}`)},
		},
	}

	u, err := h.BuildModuleCR(context.Background(), newFakeClient(), &modules.DSCContext{DSCI: dsci}, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec).Should(HaveKey("metrics"))
	metrics, ok := spec["metrics"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(metrics).Should(HaveKey("exporters"))
	g.Expect(metrics).ShouldNot(HaveKey("storage"))
}

func TestBuildModuleCR_CollectorReplicasDefaulting(t *testing.T) {
	t.Parallel()

	metricsWithStorage := &serviceApi.Metrics{
		Storage: &serviceApi.MetricsStorage{
			Size: resource.MustParse("5Gi"),
		},
	}
	traces := &serviceApi.Traces{
		Storage: serviceApi.TracesStorage{Backend: serviceApi.StorageBackendPV},
	}

	tests := []struct {
		name           string
		metrics        *serviceApi.Metrics
		traces         *serviceApi.Traces
		replicas       int32
		infra          *configv1.Infrastructure
		wantReplicas   any
		wantReplicasOn bool
	}{
		{
			name:           "multi-node defaults to 2 when metrics enabled",
			metrics:        metricsWithStorage,
			infra:          newInfrastructure(configv1.HighlyAvailableTopologyMode),
			wantReplicas:   int64(2),
			wantReplicasOn: true,
		},
		{
			name:           "SNO defaults to 1 when metrics enabled",
			metrics:        metricsWithStorage,
			infra:          newInfrastructure(configv1.SingleReplicaTopologyMode),
			wantReplicas:   int64(1),
			wantReplicasOn: true,
		},
		{
			name:           "multi-node defaults to 2 when traces enabled",
			traces:         traces,
			infra:          newInfrastructure(configv1.HighlyAvailableTopologyMode),
			wantReplicas:   int64(2),
			wantReplicasOn: true,
		},
		{
			name:           "explicit collectorReplicas is preserved",
			metrics:        metricsWithStorage,
			replicas:       5,
			infra:          newInfrastructure(configv1.SingleReplicaTopologyMode),
			wantReplicas:   int64(5),
			wantReplicasOn: true,
		},
		{
			name:           "omitted when neither metrics nor traces are enabled",
			wantReplicasOn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			h := monitoring.NewHandler()
			dsci := newDSCI(operatorv1.Managed)
			dsci.Spec.Monitoring.Metrics = tt.metrics
			dsci.Spec.Monitoring.Traces = tt.traces
			dsci.Spec.Monitoring.CollectorReplicas = tt.replicas

			var objs []client.Object
			if tt.infra != nil {
				objs = append(objs, tt.infra)
			}

			u, err := h.BuildModuleCR(context.Background(), newFakeClient(objs...), &modules.DSCContext{DSCI: dsci}, nil)
			g.Expect(err).ShouldNot(HaveOccurred())

			spec, ok := u.Object["spec"].(map[string]any)
			g.Expect(ok).Should(BeTrue())
			if tt.wantReplicasOn {
				g.Expect(spec["collectorReplicas"]).Should(Equal(tt.wantReplicas))
			} else {
				g.Expect(spec).ShouldNot(HaveKey("collectorReplicas"))
			}
		})
	}
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

func TestGetOperatorManifests_InjectsMonitoringNamespace(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	platform := &modules.PlatformContext{
		ApplicationsNamespace: "redhat-ods-applications",
		MonitoringNamespace:   "redhat-ods-monitoring",
		ChartsBasePath:        "/opt/charts",
	}

	manifests := h.GetOperatorManifests(platform)
	g.Expect(manifests.HelmCharts).Should(HaveLen(1))
	g.Expect(manifests.HelmCharts[0].ReleaseName).Should(Equal("odh-observability"))
	g.Expect(manifests.HelmCharts[0].Chart).Should(Equal(
		filepath.Join("/opt/charts", "odh-observability"),
	))

	vals, err := manifests.HelmCharts[0].Values(context.Background())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(vals["operatorNamespace"]).Should(Equal("redhat-ods-applications"))
	g.Expect(vals["monitoringNamespace"]).Should(Equal("redhat-ods-monitoring"))
}

func TestGetOperatorManifests_OmitsEmptyMonitoringNamespace(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	platform := &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		ChartsBasePath:        "/opt/charts",
	}

	manifests := h.GetOperatorManifests(platform)
	g.Expect(manifests.HelmCharts).Should(HaveLen(1))

	vals, err := manifests.HelmCharts[0].Values(context.Background())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(vals["operatorNamespace"]).Should(Equal("opendatahub"))
	g.Expect(vals).ShouldNot(HaveKey("monitoringNamespace"))
}
