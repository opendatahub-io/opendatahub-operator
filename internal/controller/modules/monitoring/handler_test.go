package monitoring_test

import (
	"context"
	"path/filepath"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
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
	g.Expect(h.IsEnabled(&configApi.PlatformModules{})).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestPopulatePlatformModule_Managed(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	pm := &configApi.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSCI: newDSCI(operatorv1.Managed)})
	g.Expect(pm.Monitoring.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_EmptyDefaultsToRemoved(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	pm := &configApi.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSCI: newDSCI("")})
	g.Expect(pm.Monitoring.ManagementState).Should(Equal(operatorv1.Removed))
}

func TestPopulatePlatformModule_NilGuards(t *testing.T) {
	h := monitoring.NewHandler()
	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configApi.PlatformModules{}, nil)
	h.PopulatePlatformModule(&configApi.PlatformModules{}, &modules.DSCContext{})
}

func TestBuildModuleCR_NilDSCIReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := monitoring.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ConsistOf(
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
		"RELATED_IMAGE_OSE_PROM_LABEL_PROXY_IMAGE",
		"RELATED_IMAGE_PERSES_IMAGE",
		"RELATED_IMAGE_KORREL8R_IMAGE",
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
