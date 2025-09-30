// This file contains shared helper functions for dashboard controller tests.
// These functions are used across multiple test files to reduce code duplication.
package dashboard

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

// TestData contains test fixtures and configuration values.
const (
	TestPath = "/test/path"

	TestManifestURIInternal   = "https://example.com/manifests.tar.gz"
	TestPlatform              = "test-platform"
	TestSelfManagedPlatform   = "self-managed"
	TestNamespace             = "test-namespace"
	TestProfile               = "test-profile"
	TestDisplayName           = "Test Display Name"
	TestDescription           = "Test Description"
	TestDomain                = "apps.example.com"
	TestRouteHost             = "odh-dashboard-test-namespace.apps.example.com"
	TestCustomPath            = "/custom/path"
	AnacondaSecretName        = "anaconda-access-secret"
	ErrorDownloadingManifests = "error downloading manifests"
	NodeTypeKey               = "node-type"
	NvidiaGPUKey              = "nvidia.com/gpu"
	DashboardHWPCRDName       = "dashboardhardwareprofiles.dashboard.opendatahub.io"
)

// ErrorMessages contains error message templates for test assertions.
const (
	errorFailedToUpdate              = "failed to update"
	ErrorFailedToUpdateParams        = "failed to update params.env"
	errorFailedToUpdateImages        = "failed to update images on path"
	errorFailedToUpdateModularImages = "failed to update modular-architecture images on path"
	errorInitPanicked                = "Init panicked with platform %s: %v"
	ErrorFailedToSetVariable         = "failed to set variable"
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

// setupTempManifestPath sets odhdeploy.DefaultManifestPath to a new temporary
// directory for the duration of the test and restores the original path when the
// test completes.
func setupTempManifestPath(t *testing.T) {
	t.Helper()
	oldDeployPath := odhdeploy.DefaultManifestPath
	t.Cleanup(func() {
		odhdeploy.DefaultManifestPath = oldDeployPath
	})
	odhdeploy.DefaultManifestPath = t.TempDir()
}

// runDevFlagsTestCases executes a table of DevFlags-related subtests using the provided testing context.
// Each test case must provide:
//  - setupDashboard: constructs the Dashboard instance for the test.
//  - setupRR: builds a ReconciliationRequest from the Dashboard.
//  - expectError: whether devFlags is expected to return an error.
//  - errorContains: optional substring that must appear in the error message when expectError is true.
//  - validateResult: optional callback to assert post-call state on the ReconciliationRequest.
func runDevFlagsTestCases(t *testing.T, ctx context.Context, testCases []struct {
	name           string
	setupDashboard func() *componentApi.Dashboard
	setupRR        func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest
	expectError    bool
	errorContains  string
	validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
}) {
	t.Helper()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			dashboard := tc.setupDashboard()
			rr := tc.setupRR(dashboard)

			err := devFlags(ctx, rr)

			if tc.expectError {
				g.Expect(err).Should(gomega.HaveOccurred())
				if tc.errorContains != "" {
					g.Expect(err.Error()).Should(gomega.ContainSubstring(tc.errorContains))
				}
			} else {
				g.Expect(err).ShouldNot(gomega.HaveOccurred())
			}

			if tc.validateResult != nil {
				tc.validateResult(t, rr)
			}
		})
	}
}

// createTestClient creates and returns a fake Kubernetes client for use in tests.
// It fails the test if the fake client cannot be created.
func createTestClient(t *testing.T) client.Client {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())
	return cli
}

// createTestDashboard creates a basic dashboard instance for testing.
func createTestDashboard() *componentApi.Dashboard {
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

// If devFlags is nil the returned Dashboard's DevFlags field will be nil.
func createTestDashboardWithCustomDevFlags(devFlags *common.DevFlags) *componentApi.Dashboard {
	return &componentApi.Dashboard{
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: devFlags,
				},
			},
		},
	}
}

// createTestDSCI creates a DSCInitialization with ApplicationsNamespace set to TestNamespace for use in tests.
func createTestDSCI() *dsciv1.DSCInitialization {
	return &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: TestNamespace,
		},
	}
}

// createTestReconciliationRequest constructs a ReconciliationRequest containing the provided client, Dashboard instance, DSCInitialization, and Release.
func createTestReconciliationRequest(cli client.Client, dashboard *componentApi.Dashboard, dsci *dsciv1.DSCInitialization, release common.Release) *odhtypes.ReconciliationRequest {
	return &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboard,
		DSCI:     dsci,
		Release:  release,
	}
}

// createTestReconciliationRequestWithManifests creates a ReconciliationRequest populated with the given client, dashboard, DSCInitialization, release, and manifests for use in tests.
func createTestReconciliationRequestWithManifests(
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

// validateSecretProperties verifies common properties of a Secret and fails the test if they do not match expectations.
// It checks that apiVersion is "v1", kind is "Secret", the object's name and namespace match expectedName and expectedNamespace,
// and that the object's "type" field exists and equals "Opaque".
func validateSecretProperties(t *testing.T, secret *unstructured.Unstructured, expectedName, expectedNamespace string) {
	t.Helper()
	g := gomega.NewWithT(t)
	g.Expect(secret.GetAPIVersion()).Should(gomega.Equal("v1"))
	g.Expect(secret.GetKind()).Should(gomega.Equal("Secret"))
	g.Expect(secret.GetName()).Should(gomega.Equal(expectedName))
	g.Expect(secret.GetNamespace()).Should(gomega.Equal(expectedNamespace))

	// Check the type field in the object
	secretType, found, err := unstructured.NestedString(secret.Object, "type")
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	g.Expect(found).Should(gomega.BeTrue())
	g.Expect(secretType).Should(gomega.Equal("Opaque"))
}

// assertPanics is a helper function that verifies a function call panics.
// It takes a testing.T, a function to call, and a descriptive message.
// assertPanics reports a test failure if fn does not panic; otherwise it logs the recovered value with the provided message.
func assertPanics(t *testing.T, fn func(), message string) {
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

// SetupTestReconciliationRequestSimple creates a minimal ReconciliationRequest for tests.
// 
// The returned request has:
// - a DSCInitialization with ApplicationsNamespace set to TestNamespace,
// - a Release with Name set to cluster.OpenDataHub.
// The Client and Instance fields are nil.
func SetupTestReconciliationRequestSimple(t *testing.T) *odhtypes.ReconciliationRequest {
	t.Helper()
	return &odhtypes.ReconciliationRequest{
		Client:   nil,
		Instance: nil,
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: TestNamespace,
			},
		},
		Release: common.Release{Name: cluster.OpenDataHub},
	}
}

// CreateTestDashboardHardwareProfile creates a test dashboard hardware profile.
// CreateTestDashboardHardwareProfile returns a DashboardHardwareProfile configured for tests.
// The returned profile has Name set to TestProfile, Namespace set to TestNamespace, DisplayName set to TestDisplayName, Enabled true, and Description set to TestDescription.
func CreateTestDashboardHardwareProfile() *DashboardHardwareProfile {
	return &DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
		},
	}
}
