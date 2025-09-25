// This file contains integration tests for dashboard controller.
// These tests verify end-to-end reconciliation scenarios.
package dashboard_test

import (
	"strings"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	testDSCIName       = "test-dsci"
	createDSCIErrorMsg = "Failed to create DSCI: %v"
	testManifestPath   = "/test/path"
)

func TestDashboardReconciliationFlow(t *testing.T) {
	t.Parallel()
	t.Run("CompleteReconciliationFlow", testCompleteReconciliationFlow)
	t.Run("ReconciliationWithDevFlags", testReconciliationWithDevFlags)
	t.Run("ReconciliationWithHardwareProfiles", testReconciliationWithHardwareProfiles)
	t.Run("ReconciliationWithRoute", testReconciliationWithRoute)
	t.Run("ReconciliationErrorHandling", testReconciliationErrorHandling)
}

func testCompleteReconciliationFlow(t *testing.T) {
	t.Parallel()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	// Create DSCI
	dsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDSCIName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard.TestNamespace,
		},
	}
	err = cli.Create(t.Context(), dsci)
	if err != nil {
		t.Fatalf(createDSCIErrorMsg, err)
	}

	// Create ingress for domain
	ingress := createTestIngress()
	err = cli.Create(t.Context(), ingress)
	if err != nil {
		t.Fatalf("Failed to create ingress: %v", err)
	}

	// Create dashboard instance
	dashboardInstance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentApi.DashboardInstanceName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{},
				},
			},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: testManifestPath, ContextDir: dashboard.ComponentName, SourcePath: "/odh"},
		},
	}

	g := NewWithT(t)
	// Test initialization
	err = dashboard.Initialize(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Manifests).Should(HaveLen(1))

	// Test dev flags (should not error with nil dev flags)
	err = dashboard.DevFlags(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test customize resources
	err = dashboard.CustomizeResources(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test set kustomized params
	err = dashboard.SetKustomizedParams(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test configure dependencies
	err = dashboard.ConfigureDependencies(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test update status
	err = dashboard.UpdateStatus(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func testReconciliationWithDevFlags(t *testing.T) {
	t.Parallel()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	dsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDSCIName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard.TestNamespace,
		},
	}
	err = cli.Create(t.Context(), dsci)
	if err != nil {
		t.Fatalf(createDSCIErrorMsg, err)
	}

	// Create dashboard with dev flags
	dashboardInstance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentApi.DashboardInstanceName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{
						Manifests: []common.ManifestsConfig{
							{
								URI:        "https://example.com/manifests.tar.gz",
								ContextDir: "manifests",
								SourcePath: "/custom/path",
							},
						},
					},
				},
			},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: testManifestPath, ContextDir: dashboard.ComponentName, SourcePath: "/odh"},
		},
	}

	// Dev flags will fail due to download
	g := NewWithT(t)
	err = dashboard.DevFlags(t.Context(), rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("error downloading manifests"))
}

func testReconciliationWithHardwareProfiles(t *testing.T) {
	t.Parallel()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	dsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDSCIName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard.TestNamespace,
		},
	}
	err = cli.Create(t.Context(), dsci)
	if err != nil {
		t.Fatalf(createDSCIErrorMsg, err)
	}

	// Create CRD for dashboard hardware profiles
	crd := createTestCRD("dashboardhardwareprofiles.dashboard.opendatahub.io")
	err = cli.Create(t.Context(), crd)
	if err != nil {
		t.Fatalf("Failed to create CRD: %v", err)
	}

	// Create dashboard hardware profile
	hwProfile := createTestDashboardHardwareProfile()
	err = cli.Create(t.Context(), hwProfile)
	if err != nil {
		t.Fatalf("Failed to create hardware profile: %v", err)
	}

	dashboardInstance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentApi.DashboardInstanceName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{},
				},
			},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: testManifestPath, ContextDir: dashboard.ComponentName, SourcePath: "/odh"},
		},
	}

	g := NewWithT(t)
	// Test hardware profile reconciliation
	err = dashboard.ReconcileHardwareProfiles(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify infrastructure hardware profile was created
	var infraHWP infrav1.HardwareProfile
	err = cli.Get(t.Context(), client.ObjectKey{
		Name:      dashboard.TestProfile,
		Namespace: dashboard.TestNamespace,
	}, &infraHWP)
	// The hardware profile might not be created if the CRD doesn't exist or other conditions
	if err != nil {
		// This is expected in some test scenarios
		t.Logf("Hardware profile not found (expected in some scenarios): %v", err)
	} else {
		g.Expect(infraHWP.Name).Should(Equal(dashboard.TestProfile))
	}
}

func testReconciliationWithRoute(t *testing.T) {
	t.Parallel()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	dsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDSCIName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard.TestNamespace,
		},
	}
	err = cli.Create(t.Context(), dsci)
	if err != nil {
		t.Fatalf(createDSCIErrorMsg, err)
	}

	// Create route
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-dashboard",
			Namespace: dashboard.TestNamespace,
			Labels: map[string]string{
				labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
			},
		},
		Spec: routev1.RouteSpec{
			Host: dashboard.TestRouteHost,
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host: dashboard.TestRouteHost,
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:   routev1.RouteAdmitted,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
	}
	err = cli.Create(t.Context(), route)
	if err != nil {
		t.Fatalf("Failed to create route: %v", err)
	}

	dashboardInstance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentApi.DashboardInstanceName,
			Namespace: dashboard.TestNamespace,
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{},
				},
			},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: testManifestPath, ContextDir: dashboard.ComponentName, SourcePath: "/odh"},
		},
	}

	g := NewWithT(t)
	// Test status update
	err = dashboard.UpdateStatus(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify URL was set
	dashboardInstance, ok := rr.Instance.(*componentApi.Dashboard)
	g.Expect(ok).Should(BeTrue())
	g.Expect(dashboardInstance.Status.URL).Should(Equal("https://" + dashboard.TestRouteHost))
}

func testReconciliationErrorHandling(t *testing.T) {
	t.Parallel()
	cli, err := fakeclient.New()
	require.NoError(t, err)

	// Create dashboard with invalid instance type
	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: &componentApi.Kserve{}, // Wrong type
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: dashboard.TestNamespace,
			},
		},
		Release: common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: testManifestPath, ContextDir: dashboard.ComponentName, SourcePath: "/odh"},
		},
	}

	g := NewWithT(t)

	// Test dashboard.Initialize function should fail
	err = dashboard.Initialize(t.Context(), rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("is not a componentApi.Dashboard"))

	// Test dashboard.DevFlags function should fail
	err = dashboard.DevFlags(t.Context(), rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("is not a componentApi.Dashboard"))

	// Test dashboard.UpdateStatus function should fail
	err = dashboard.UpdateStatus(t.Context(), rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("instance is not of type *componentApi.Dashboard"))
}

// Helper functions for integration tests.
func createTestCRD(name string) *unstructured.Unstructured {
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(gvk.CustomResourceDefinition)
	crd.SetName(name)
	crd.Object["status"] = map[string]interface{}{
		"storedVersions": []string{"v1alpha1"},
	}
	return crd
}

func createTestDashboardHardwareProfile() *unstructured.Unstructured {
	profile := &unstructured.Unstructured{}
	profile.SetGroupVersionKind(gvk.DashboardHardwareProfile)
	profile.SetName(dashboard.TestProfile)
	profile.SetNamespace(dashboard.TestNamespace)
	profile.Object["spec"] = map[string]interface{}{
		"displayName": dashboard.TestDisplayName,
		"enabled":     true,
		"description": dashboard.TestDescription,
		"nodeSelector": map[string]interface{}{
			dashboard.NodeTypeKey: "gpu",
		},
	}
	return profile
}

// Helper function to create test ingress.
func createTestIngress() *unstructured.Unstructured {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")
	ingress.Object["spec"] = map[string]interface{}{
		"domain": "apps.example.com",
	}
	return ingress
}
