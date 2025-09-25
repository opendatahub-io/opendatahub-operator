// Consolidated tests for dashboard controller parameter functionality.
// This file provides comprehensive test coverage for SetKustomizedParams function,
// combining and improving upon the test coverage from the original parameter tests.
package dashboard_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dashboardctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard/dashboard_test"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

const (
	consolidatedParamsEnvFileName = "params.env"
	odhDashboardURLPrefix         = "https://odh-dashboard-"
	rhodsDashboardURLPrefix       = "https://rhods-dashboard-"
)

// setupTempDirWithParamsConsolidated creates a temporary directory with the proper structure and params.env file.
func setupTempDirWithParamsConsolidated(t *testing.T) string {
	t.Helper()
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create the directory structure that matches the manifest path
	manifestDir := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh")
	err := os.MkdirAll(manifestDir, 0755)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Create a params.env file in the manifest directory
	paramsEnvPath := filepath.Join(manifestDir, consolidatedParamsEnvFileName)
	paramsEnvContent := dashboard_test.InitialParamsEnvContent
	err = os.WriteFile(paramsEnvPath, []byte(paramsEnvContent), 0600)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	return tempDir
}

// createIngressResourceConsolidated creates an ingress resource with the test domain.
func createIngressResourceConsolidated(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err := unstructured.SetNestedField(ingress.Object, dashboard_test.TestDomain, "spec", "domain")
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	return ingress
}

// createFakeClientWithIngressConsolidated creates a fake client with an ingress resource.
func createFakeClientWithIngressConsolidated(t *testing.T, ingress *unstructured.Unstructured) client.Client {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	err = cli.Create(t.Context(), ingress)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	return cli
}

// createFakeClientWithoutIngressConsolidated creates a fake client without an ingress resource.
func createFakeClientWithoutIngressConsolidated(t *testing.T) client.Client {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())
	return cli
}

// verifyParamsEnvModifiedConsolidated checks that the params.env file was actually modified with expected values.
func verifyParamsEnvModifiedConsolidated(t *testing.T, tempDir string, expectedURL, expectedTitle string) {
	t.Helper()
	g := gomega.NewWithT(t)

	paramsEnvPath := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh", consolidatedParamsEnvFileName)
	content, err := os.ReadFile(paramsEnvPath)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	contentStr := string(content)
	g.Expect(contentStr).Should(gomega.ContainSubstring(expectedURL))
	g.Expect(contentStr).Should(gomega.ContainSubstring(expectedTitle))

	// Verify the content is different from initial content
	g.Expect(contentStr).ShouldNot(gomega.Equal(dashboard_test.InitialParamsEnvContent))
}

// testCase represents a single test case for SetKustomizedParams function.
type testCase struct {
	name           string
	setupFunc      func(t *testing.T) *odhtypes.ReconciliationRequest
	expectedError  bool
	errorSubstring string
	verifyFunc     func(t *testing.T, rr *odhtypes.ReconciliationRequest)
}

// getComprehensiveTestCases returns all test cases for SetKustomizedParams function.
func getComprehensiveTestCases() []testCase {
	return []testCase{
		{
			name: "BasicSuccess",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParamsConsolidated(t)
				ingress := createIngressResourceConsolidated(t)
				cli := createFakeClientWithIngressConsolidated(t, ingress)

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "odh"},
				}
				return rr
			},
			expectedError: false,
			verifyFunc: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				expectedURL := odhDashboardURLPrefix + dashboard_test.TestNamespace + "." + dashboard_test.TestDomain
				expectedTitle := dashboardctrl.SectionTitle[cluster.OpenDataHub]
				verifyParamsEnvModifiedConsolidated(t, rr.Manifests[0].Path, expectedURL, expectedTitle)
			},
		},
		{
			name: "MissingIngress",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParamsConsolidated(t)
				cli := createFakeClientWithoutIngressConsolidated(t)

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "odh"},
				}
				return rr
			},
			expectedError:  true,
			errorSubstring: dashboard_test.ErrorFailedToSetVariable,
		},
		{
			name: "EmptyManifests",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				ingress := createIngressResourceConsolidated(t)
				cli := createFakeClientWithIngressConsolidated(t, ingress)

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{} // Empty manifests
				return rr
			},
			expectedError:  true,
			errorSubstring: "no manifests available",
		},
		{
			name: "NilManifests",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				ingress := createIngressResourceConsolidated(t)
				cli := createFakeClientWithIngressConsolidated(t, ingress)

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = nil // Nil manifests
				return rr
			},
			expectedError:  true,
			errorSubstring: "no manifests available",
		},
		{
			name: "InvalidManifestPath",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				ingress := createIngressResourceConsolidated(t)
				cli := createFakeClientWithIngressConsolidated(t, ingress)

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: "/invalid/path", ContextDir: dashboardctrl.ComponentName, SourcePath: "odh"},
				}
				return rr
			},
			expectedError: false, // ApplyParams handles missing files gracefully by returning nil
		},
		{
			name: "MultipleManifests",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParamsConsolidated(t)
				ingress := createIngressResourceConsolidated(t)
				cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "odh"},
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "bff"},
				}
				return rr
			},
			expectedError: false, // Should work with multiple manifests (uses first one)
			verifyFunc: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				expectedURL := odhDashboardURLPrefix + dashboard_test.TestNamespace + "." + dashboard_test.TestDomain
				expectedTitle := dashboardctrl.SectionTitle[cluster.OpenDataHub]
				verifyParamsEnvModifiedConsolidated(t, rr.Manifests[0].Path, expectedURL, expectedTitle)
			},
		},
		{
			name: "DifferentReleases_SelfManagedRhoai",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParamsConsolidated(t)
				ingress := createIngressResourceConsolidated(t)
				cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Release = common.Release{Name: cluster.SelfManagedRhoai}
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "odh"},
				}
				return rr
			},
			expectedError: false,
			verifyFunc: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				expectedURL := rhodsDashboardURLPrefix + dashboard_test.TestNamespace + "." + dashboard_test.TestDomain
				expectedTitle := "OpenShift Self Managed Services"
				verifyParamsEnvModifiedConsolidated(t, rr.Manifests[0].Path, expectedURL, expectedTitle)
			},
		},
		{
			name: "DifferentReleases_ManagedRhoai",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParamsConsolidated(t)
				ingress := createIngressResourceConsolidated(t)
				cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Release = common.Release{Name: cluster.ManagedRhoai}
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "odh"},
				}
				return rr
			},
			expectedError: false,
			verifyFunc: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				expectedURL := rhodsDashboardURLPrefix + dashboard_test.TestNamespace + "." + dashboard_test.TestDomain
				expectedTitle := dashboardctrl.SectionTitle[cluster.ManagedRhoai]
				verifyParamsEnvModifiedConsolidated(t, rr.Manifests[0].Path, expectedURL, expectedTitle)
			},
		},
		{
			name: "NilDSCI",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				ingress := createIngressResourceConsolidated(t)
				cli := createFakeClientWithIngressConsolidated(t, ingress)

				dashboardInstance := &componentApi.Dashboard{}
				rr := &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					DSCI:     nil, // Nil DSCI
					Release:  common.Release{Name: cluster.OpenDataHub},
					Manifests: []odhtypes.ManifestInfo{
						{Path: dashboard_test.TestPath, ContextDir: dashboardctrl.ComponentName, SourcePath: "odh"},
					},
				}
				return rr
			},
			expectedError:  true,
			errorSubstring: "runtime error: invalid memory address or nil pointer dereference", // Panic converted to error
		},
	}
}

// runTestWithPanicRecovery runs a test function with panic recovery for consistent error handling.
func runTestWithPanicRecovery(t *testing.T, testFunc func() error) error {
	t.Helper()
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Convert panic to error for consistent testing
				err = fmt.Errorf("panic occurred: %v", r)
			}
		}()
		err = testFunc()
	}()
	return err
}

// TestSetKustomizedParamsComprehensive provides comprehensive test coverage for SetKustomizedParams function.
func TestSetKustomizedParamsComprehensive(t *testing.T) {
	tests := getComprehensiveTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			g := gomega.NewWithT(t)

			rr := tt.setupFunc(t)

			// Use panic recovery for consistent error handling
			err := runTestWithPanicRecovery(t, func() error {
				return dashboardctrl.SetKustomizedParams(ctx, rr)
			})

			if tt.expectedError {
				g.Expect(err).Should(gomega.HaveOccurred())
				if tt.errorSubstring != "" {
					g.Expect(err.Error()).Should(gomega.ContainSubstring(tt.errorSubstring))
				}
			} else {
				g.Expect(err).ShouldNot(gomega.HaveOccurred())
				// Run verification function if provided
				if tt.verifyFunc != nil {
					tt.verifyFunc(t, rr)
				}
			}
		})
	}
}
