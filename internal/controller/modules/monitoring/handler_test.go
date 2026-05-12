package monitoring_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC:                   &dscv2.DataScienceCluster{},
		DSCI: &dsciv2.DSCInitialization{
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
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	h := monitoring.NewHandler()
	if !h.IsEnabled(newPlatformCtx(operatorv1.Managed)) {
		t.Error("expected monitoring to be enabled when ManagementState is Managed")
	}
}

func TestIsEnabled_Removed(t *testing.T) {
	h := monitoring.NewHandler()
	if h.IsEnabled(newPlatformCtx(operatorv1.Removed)) {
		t.Error("expected monitoring to be disabled when ManagementState is Removed")
	}
}

func TestIsEnabled_Empty(t *testing.T) {
	h := monitoring.NewHandler()
	if h.IsEnabled(newPlatformCtx("")) {
		t.Error("expected monitoring to be disabled when ManagementState is empty")
	}
}

func TestIsEnabled_NilDSCI(t *testing.T) {
	h := monitoring.NewHandler()
	ctx := &modules.PlatformContext{DSC: &dscv2.DataScienceCluster{}}
	if h.IsEnabled(ctx) {
		t.Error("expected monitoring to be disabled when DSCI is nil")
	}
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	h := monitoring.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	if err != nil {
		t.Fatalf("BuildModuleCR returned error: %v", err)
	}

	if u.GetName() != serviceApi.MonitoringInstanceName {
		t.Errorf("name: want %q, got %q", serviceApi.MonitoringInstanceName, u.GetName())
	}
	if u.GetKind() != serviceApi.MonitoringKind {
		t.Errorf("kind: want %q, got %q", serviceApi.MonitoringKind, u.GetKind())
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}

	if got := spec["managementState"]; got != "Managed" {
		t.Errorf("managementState: want %q, got %v", "Managed", got)
	}
	if got := spec["namespace"]; got != "opendatahub" {
		t.Errorf("namespace: want %q, got %v", "opendatahub", got)
	}
}

func TestBuildModuleCR_EmptyManagementStateDefaultsToManaged(t *testing.T) {
	h := monitoring.NewHandler()
	platform := newPlatformCtx("")

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	if err != nil {
		t.Fatalf("BuildModuleCR returned error: %v", err)
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}

	if got := spec["managementState"]; got != "Managed" {
		t.Errorf("managementState: want %q, got %v", "Managed", got)
	}
}

func TestBuildModuleCR_ProjectsMetrics(t *testing.T) {
	h := monitoring.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSCI.Spec.Monitoring.Metrics = &serviceApi.Metrics{
		Storage: &serviceApi.MetricsStorage{
			Size:      resource.MustParse("10Gi"),
			Retention: "7d",
		},
		Replicas: 2,
	}

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	if err != nil {
		t.Fatalf("BuildModuleCR returned error: %v", err)
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}
	metrics, ok := spec["metrics"].(map[string]any)
	if !ok {
		t.Fatal("spec.metrics missing")
	}

	storage, ok := metrics["storage"].(map[string]any)
	if !ok {
		t.Fatal("spec.metrics.storage missing")
	}
	if got := storage["size"]; got != "10Gi" {
		t.Errorf("metrics.storage.size: want %q, got %v", "10Gi", got)
	}
	if got := storage["retention"]; got != "7d" {
		t.Errorf("metrics.storage.retention: want %q, got %v", "7d", got)
	}
	if got := metrics["replicas"]; got != int64(2) {
		t.Errorf("metrics.replicas: want 2, got %v", got)
	}
}

func TestBuildModuleCR_ProjectsTraces(t *testing.T) {
	h := monitoring.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSCI.Spec.Monitoring.Traces = &serviceApi.Traces{
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

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	if err != nil {
		t.Fatalf("BuildModuleCR returned error: %v", err)
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}
	traces, ok := spec["traces"].(map[string]any)
	if !ok {
		t.Fatal("spec.traces missing")
	}

	storage, ok := traces["storage"].(map[string]any)
	if !ok {
		t.Fatal("spec.traces.storage missing")
	}
	if got := storage["backend"]; got != "s3" {
		t.Errorf("traces.storage.backend: want %q, got %v", "s3", got)
	}
	if got := storage["secret"]; got != "my-s3-creds" {
		t.Errorf("traces.storage.secret: want %q, got %v", "my-s3-creds", got)
	}

	if got := traces["sampleRatio"]; got != "0.5" {
		t.Errorf("traces.sampleRatio: want %q, got %v", "0.5", got)
	}

	tls, ok := traces["tls"].(map[string]any)
	if !ok {
		t.Fatal("spec.traces.tls missing")
	}
	if got := tls["enabled"]; got != true {
		t.Errorf("traces.tls.enabled: want true, got %v", got)
	}
}

func TestBuildModuleCR_NilDSCIReturnsError(t *testing.T) {
	h := monitoring.NewHandler()
	platform := &modules.PlatformContext{DSC: &dscv2.DataScienceCluster{}}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	if err == nil {
		t.Error("expected error when DSCI is nil")
	}
}

func TestGetRelatedImages(t *testing.T) {
	h := monitoring.NewHandler()
	images := h.GetRelatedImages()

	want := map[string]bool{
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE":  false,
		"RELATED_IMAGE_OSE_PROM_LABEL_PROXY_IMAGE": false,
		"RELATED_IMAGE_CLI_IMAGE":                  false,
		"RELATED_IMAGE_PERSES_IMAGE":               false,
	}

	for _, img := range images {
		if _, ok := want[img]; ok {
			want[img] = true
		} else {
			t.Errorf("unexpected related image: %q", img)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing related image: %q", name)
		}
	}
}

func TestGetName(t *testing.T) {
	h := monitoring.NewHandler()
	if got := h.GetName(); got != serviceApi.MonitoringServiceName {
		t.Errorf("GetName: want %q, got %q", serviceApi.MonitoringServiceName, got)
	}
}
