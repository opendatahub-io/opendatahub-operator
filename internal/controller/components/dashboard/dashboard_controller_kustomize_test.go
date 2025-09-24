// Black-box tests that run as dashboard_test package and validate exported/public functions
package dashboard_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dashboardctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

// setupTempDirWithParams creates a temporary directory with the proper structure and params.env file.
func setupTempDirWithParams(t *testing.T) string {
	t.Helper()
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create the directory structure that matches the manifest path
	manifestDir := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh")
	err := os.MkdirAll(manifestDir, 0755)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Create a params.env file in the manifest directory
	paramsEnvPath := filepath.Join(manifestDir, paramsEnvFileName)
	paramsEnvContent := dashboardctrl.InitialParamsEnvContent
	err = os.WriteFile(paramsEnvPath, []byte(paramsEnvContent), 0600)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	return tempDir
}

// createIngressResource creates an ingress resource with the test domain.
func createIngressResource(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err := unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	return ingress
}

// createFakeClientWithIngress creates a fake client with an ingress resource.
func createFakeClientWithIngress(t *testing.T, ingress *unstructured.Unstructured) client.Client {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	err = cli.Create(t.Context(), ingress)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	return cli
}

// verifyParamsEnvModified checks that the params.env file was actually modified with expected values.
func verifyParamsEnvModified(t *testing.T, tempDir string, expectedURL, expectedTitle string) {
	t.Helper()
	g := gomega.NewWithT(t)

	paramsEnvPath := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh", paramsEnvFileName)
	content, err := os.ReadFile(paramsEnvPath)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	contentStr := string(content)
	g.Expect(contentStr).Should(gomega.ContainSubstring(expectedURL))
	g.Expect(contentStr).Should(gomega.ContainSubstring(expectedTitle))

	// Verify the content is different from initial content
	g.Expect(contentStr).ShouldNot(gomega.Equal(dashboardctrl.InitialParamsEnvContent))
}

func TestSetKustomizedParamsTable(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) *odhtypes.ReconciliationRequest
		expectedError  bool
		errorSubstring string
		verifyFunc     func(t *testing.T, rr *odhtypes.ReconciliationRequest) // Optional verification function
	}{
		{
			name: "Basic",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParams(t)
				ingress := createIngressResource(t)
				cli := createFakeClientWithIngress(t, ingress)

				rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
				}
				return rr
			},
			expectedError: false,
			verifyFunc: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				// Verify that the params.env file was actually modified
				expectedURL := "https://odh-dashboard-" + dashboardctrl.TestNamespace + "." + dashboardctrl.TestDomain
				expectedTitle := dashboardctrl.SectionTitle[cluster.OpenDataHub]
				verifyParamsEnvModified(t, rr.Manifests[0].Path, expectedURL, expectedTitle)
			},
		},
		{
			name: "MissingIngress",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParams(t)

				// Create a mock client that will fail to get domain
				cli, err := fakeclient.New()
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				// Don't create the ingress resource, so domain lookup will fail
				rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
				}
				return rr
			},
			expectedError:  true,
			errorSubstring: dashboardctrl.ErrorFailedToSetVariable,
		},
		{
			name: "EmptyManifests",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				ingress := createIngressResource(t)
				cli := createFakeClientWithIngress(t, ingress)

				rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
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
				ingress := createIngressResource(t)
				cli := createFakeClientWithIngress(t, ingress)

				rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
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
				ingress := createIngressResource(t)
				cli := createFakeClientWithIngress(t, ingress)

				rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: "/invalid/path", ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
				}
				return rr
			},
			expectedError: false, // ApplyParams handles missing files gracefully by returning nil
		},
		{
			name: "MultipleManifests",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParams(t)
				ingress := createIngressResource(t)
				cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/bff"},
				}
				return rr
			},
			expectedError: false, // Should work with multiple manifests (uses first one)
			verifyFunc: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				// Verify that the params.env file was actually modified
				expectedURL := "https://odh-dashboard-" + dashboardctrl.TestNamespace + "." + dashboardctrl.TestDomain
				expectedTitle := dashboardctrl.SectionTitle[cluster.OpenDataHub]
				verifyParamsEnvModified(t, rr.Manifests[0].Path, expectedURL, expectedTitle)
			},
		},
		{
			name: "DifferentReleases",
			setupFunc: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				tempDir := setupTempDirWithParams(t)
				ingress := createIngressResource(t)
				cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Release = common.Release{Name: cluster.SelfManagedRhoai}
				rr.Manifests = []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
				}
				return rr
			},
			expectedError: false, // Should work with different releases
			verifyFunc: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				// Verify that the params.env file was actually modified with SelfManagedRhoai values
				expectedURL := "https://rhods-dashboard-" + dashboardctrl.TestNamespace + "." + dashboardctrl.TestDomain
				expectedTitle := "OpenShift Self Managed Services"
				verifyParamsEnvModified(t, rr.Manifests[0].Path, expectedURL, expectedTitle)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			g := gomega.NewWithT(t)

			rr := tt.setupFunc(t)
			err := dashboardctrl.SetKustomizedParams(ctx, rr)

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
