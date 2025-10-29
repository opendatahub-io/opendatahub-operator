// This file contains shared helper functions for dashboard controller tests.
// These functions are used across multiple test files to reduce code duplication.
// Helper functions are intended for testing purposes only.
package dashboard_test

import (
	"testing"

	"github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

// getDashboardHandler returns the dashboard component handler from the registry.
//
//nolint:ireturn
func getDashboardHandler() registry.ComponentHandler {
	var handler registry.ComponentHandler
	_ = registry.ForEach(func(ch registry.ComponentHandler) error {
		if ch.GetName() == componentApi.DashboardComponentName {
			handler = ch
		}
		return nil
	})
	return handler
}

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
	NonExistentPlatform     = "non-existent-platform"
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

// SetupTempManifestPath sets up a temporary directory for manifest downloads.
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

// CreateTestDashboard creates a basic dashboard instance for testing.
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
			DashboardCommonSpec: componentApi.DashboardCommonSpec{},
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

// CreateDSCWithDashboard creates a DSC with dashboard component enabled.
func CreateDSCWithDashboard(managementState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: managementState,
					},
				},
			},
		},
	}
}

// createRoute creates a Route instance for testing.
func createRoute(name, host string, admitted bool) *routev1.Route {
	labels := map[string]string{
		"platform.opendatahub.io/part-of": "dashboard",
	}
	return createRouteWithLabels(name, host, admitted, labels)
}

// createRouteWithLabels creates a Route instance with custom labels for testing.
func createRouteWithLabels(name, host string, admitted bool, labels map[string]string) *routev1.Route {
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: TestNamespace,
			Labels:    labels,
		},
		Spec: routev1.RouteSpec{
			Host: host,
		},
	}

	if admitted {
		route.Status = routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host: host,
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:   routev1.RouteAdmitted,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		}
	}

	return route
}

// createTestReconciliationRequest creates a basic reconciliation request for testing.
func CreateTestReconciliationRequest(cli client.Client, dashboard *componentApi.Dashboard, release common.Release) *odhtypes.ReconciliationRequest {
	return &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboard,
		Release:  release,
	}
}

// createTestReconciliationRequestWithManifests creates a reconciliation request with manifests for testing.
func CreateTestReconciliationRequestWithManifests(
	cli client.Client,
	dashboard *componentApi.Dashboard,
	release common.Release,
	manifests []odhtypes.ManifestInfo,
) *odhtypes.ReconciliationRequest {
	return &odhtypes.ReconciliationRequest{
		Client:    cli,
		Instance:  dashboard,
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
	return CreateTestReconciliationRequest(cli, dashboard, common.Release{Name: cluster.OpenDataHub})
}

// CreateTestDSCI creates a test DSCI resource.
func CreateTestDSCI(namespace string) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "dscinitialization.opendatahub.io/v2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: namespace,
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: namespace,
		},
	}
}
