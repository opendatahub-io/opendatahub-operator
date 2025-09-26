// This file contains shared helper functions for dashboard controller tests.
// These functions are used across multiple test files to reduce code duplication.
// Helper functions are intended for testing purposes only.
package dashboard_test

import (
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

// TestData contains test fixtures and configuration values.
const (
	TestPath                = "/test/path"
	TestNamespace           = "test-namespace"
	TestPlatform            = "test-platform"
	TestSelfManagedPlatform = "self-managed"
	TestProfile             = "test-profile"
	TestDisplayName         = "Test Display Name"
	TestDescription         = "Test Description"
	TestDomain              = "apps.example.com"
	TestRouteHost           = "odh-dashboard-test-namespace.apps.example.com"
	NodeTypeKey             = "node-type"
	NvidiaGPUKey            = "nvidia.com/gpu"
	DashboardHWPCRDName     = "dashboardhardwareprofiles.dashboard.opendatahub.io"
)

// ErrorMessages contains error message templates for test assertions.
const (
	ErrorFailedToUpdate              = "failed to update"
	ErrorFailedToUpdateParams        = "failed to update params.env"
	ErrorInitPanicked                = "Init panicked with platform %s: %v"
	ErrorFailedToSetVariable         = "failed to set variable"
	ErrorFailedToUpdateImages        = "failed to update images on path"
	ErrorFailedToUpdateModularImages = "failed to update " + dashboard.ModularArchitectureSourcePath + " images on path"
)

// LogMessages contains log message templates for test assertions.
const (
	LogSetKustomizedParamsError = "setKustomizedParams returned error (expected): %v"
)

// initialParamsEnvContent is the standard content for params.env files in tests.
const InitialParamsEnvContent = `# Initial params.env content
dashboard-url=https://odh-dashboard-test.example.com
section-title=Test Title
`

// setupTempManifestPath sets up a temporary directory for manifest downloads.
func SetupTempManifestPath(t *testing.T) {
	t.Helper()
	oldDeployPath := odhdeploy.DefaultManifestPath
	t.Cleanup(func() {
		odhdeploy.DefaultManifestPath = oldDeployPath
	})
	odhdeploy.DefaultManifestPath = t.TempDir()
}

// createTestClient creates a fake client for testing.
func CreateTestClient(t *testing.T) client.Client {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())
	return cli
}

// createTestDashboard creates a basic dashboard instance for testing.
func CreateTestDashboard() *componentApi.Dashboard {
	return &componentApi.Dashboard{
		TypeMeta: metav1.TypeMeta{
			APIVersion: componentApi.GroupVersion.String(),
			Kind:       componentApi.DashboardKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              componentApi.DashboardInstanceName,
			Namespace:         TestNamespace,
			CreationTimestamp: metav1.Unix(1640995200, 0), // 2022-01-01T00:00:00Z
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{
						Manifests: []common.ManifestsConfig{},
					},
				},
			},
		},
		Status: componentApi.DashboardStatus{
			Status: common.Status{
				Phase:              "Ready",
				ObservedGeneration: 1,
				Conditions: []common.Condition{
					{
						Type:               componentApi.DashboardKind + "Ready",
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						LastTransitionTime: metav1.Unix(1640995200, 0), // 2022-01-01T00:00:00Z
						Reason:             "ReconcileSucceeded",
						Message:            "Dashboard is ready",
					},
				},
			},
			DashboardCommonStatus: componentApi.DashboardCommonStatus{
				URL: "https://odh-dashboard-" + TestNamespace + ".apps.example.com",
			},
		},
	}
}

// createTestDSCI creates a DSCI instance for testing.
func CreateTestDSCI() *dsciv1.DSCInitialization {
	return &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: TestNamespace,
		},
	}
}

// createTestReconciliationRequest creates a basic reconciliation request for testing.
func CreateTestReconciliationRequest(cli client.Client, dashboard *componentApi.Dashboard, dsci *dsciv1.DSCInitialization, release common.Release) *odhtypes.ReconciliationRequest {
	return &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboard,
		DSCI:     dsci,
		Release:  release,
	}
}

// createTestReconciliationRequestWithManifests creates a reconciliation request with manifests for testing.
func CreateTestReconciliationRequestWithManifests(
	cli client.Client,
	dashboard *componentApi.Dashboard,
	dsci *dsciv1.DSCInitialization,
	release common.Release,
	manifests []odhtypes.ManifestInfo,
) *odhtypes.ReconciliationRequest {
	return &odhtypes.ReconciliationRequest{
		Client:    cli,
		Instance:  dashboard,
		DSCI:      dsci,
		Release:   release,
		Manifests: manifests,
	}
}

// assertPanics is a helper function that verifies a function call panics.
func AssertPanics(t *testing.T, fn func(), message string) {
	t.Helper()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected function to panic, but it didn't. %s", message)
		} else {
			t.Logf("%s: %v", message, r)
		}
	}()

	fn()
}

// CreateTestDashboardHardwareProfile creates a test dashboard hardware profile.
func CreateTestDashboardHardwareProfile() *dashboard.DashboardHardwareProfile {
	return &dashboard.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: dashboard.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
		},
	}
}

// SetupTestReconciliationRequestSimple creates a simple test reconciliation request.
func SetupTestReconciliationRequestSimple(t *testing.T) *odhtypes.ReconciliationRequest {
	t.Helper()
	cli := CreateTestClient(t)
	dashboard := CreateTestDashboard()
	dsci := CreateTestDSCI()
	return CreateTestReconciliationRequest(cli, dashboard, dsci, common.Release{Name: cluster.OpenDataHub})
}
