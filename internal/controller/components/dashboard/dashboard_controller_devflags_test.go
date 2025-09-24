// This file contains tests for dashboard controller dev flags functionality.
// These tests verify the devFlags function and related dev flags logic.
//
//nolint:testpackage
package dashboard

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

const (
	TestCustomPath2                 = "/custom/path2"
	ErrorDownloadingManifestsPrefix = "error downloading manifests"
)

func TestDevFlagsBasicCases(t *testing.T) {
	ctx := t.Context()

	cli := createTestClient(t)
	setupTempManifestPath(t)
	dsci := createTestDSCI()

	testCases := []struct {
		name           string
		setupDashboard func() *componentApi.Dashboard
		setupRR        func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest
		expectError    bool
		errorContains  string
		validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name:           "NoDevFlagsSet",
			setupDashboard: createTestDashboard,
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return createTestReconciliationRequestWithManifests(
					cli, dashboard, dsci,
					common.Release{Name: cluster.OpenDataHub},
					[]odhtypes.ManifestInfo{
						{Path: TestPath, ContextDir: ComponentName, SourcePath: "/odh"},
					},
				)
			},
			expectError: false,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Manifests[0].Path).Should(Equal(TestPath))
			},
		},
	}

	runDevFlagsTestCases(t, ctx, testCases)
}

// TestDevFlagsWithCustomManifests tests DevFlags with custom manifest configurations.
func TestDevFlagsWithCustomManifests(t *testing.T) {
	ctx := t.Context()

	cli := createTestClient(t)
	setupTempManifestPath(t)
	dsci := createTestDSCI()

	testCases := []struct {
		name           string
		setupDashboard func() *componentApi.Dashboard
		setupRR        func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest
		expectError    bool
		errorContains  string
		validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "WithDevFlags",
			setupDashboard: func() *componentApi.Dashboard {
				return createTestDashboardWithCustomDevFlags(&common.DevFlags{
					Manifests: []common.ManifestsConfig{
						{
							URI:        "https://github.com/test/repo/tarball/main",
							ContextDir: "manifests",
							SourcePath: TestCustomPath,
						},
					},
				})
			},
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return createTestReconciliationRequestWithManifests(
					cli, dashboard, dsci,
					common.Release{Name: cluster.OpenDataHub},
					[]odhtypes.ManifestInfo{
						{Path: TestPath, ContextDir: ComponentName, SourcePath: "/odh"},
					},
				)
			},
			expectError:   true,
			errorContains: ErrorDownloadingManifests,
		},
		{
			name: "InvalidInstance",
			setupDashboard: func() *componentApi.Dashboard {
				return &componentApi.Dashboard{}
			},
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: &componentApi.Kserve{}, // Wrong type
				}
			},
			expectError:   true,
			errorContains: "is not a componentApi.Dashboard",
		},
		{
			name: "WithEmptyManifests",
			setupDashboard: func() *componentApi.Dashboard {
				return &componentApi.Dashboard{
					Spec: componentApi.DashboardSpec{
						DashboardCommonSpec: componentApi.DashboardCommonSpec{
							DevFlagsSpec: common.DevFlagsSpec{
								DevFlags: &common.DevFlags{
									Manifests: []common.ManifestsConfig{
										{
											SourcePath: TestCustomPath,
											URI:        TestManifestURIInternal,
										},
									},
								},
							},
						},
					},
				}
			},
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:    cli,
					Instance:  dashboard,
					DSCI:      dsci,
					Release:   common.Release{Name: cluster.OpenDataHub},
					Manifests: []odhtypes.ManifestInfo{}, // Empty manifests
				}
			},
			expectError:   true,
			errorContains: ErrorDownloadingManifests,
		},
		{
			name: "WithMultipleManifests",
			setupDashboard: func() *componentApi.Dashboard {
				return &componentApi.Dashboard{
					Spec: componentApi.DashboardSpec{
						DashboardCommonSpec: componentApi.DashboardCommonSpec{
							DevFlagsSpec: common.DevFlagsSpec{
								DevFlags: &common.DevFlags{
									Manifests: []common.ManifestsConfig{
										{
											SourcePath: "/custom/path1",
											URI:        "https://example.com/manifests1.tar.gz",
										},
										{
											SourcePath: "/custom/path2",
											URI:        "https://example.com/manifests2.tar.gz",
										},
									},
								},
							},
						},
					},
				}
			},
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboard,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.OpenDataHub},
					Manifests: []odhtypes.ManifestInfo{
						{Path: TestPath, ContextDir: ComponentName, SourcePath: "/odh"},
						{Path: TestPath, ContextDir: ComponentName, SourcePath: "/bff"},
					},
				}
			},
			expectError:   true,
			errorContains: ErrorDownloadingManifests,
		},
	}

	runDevFlagsTestCases(t, ctx, testCases)
}

// TestDevFlagsWithEmptyManifests tests DevFlags with empty manifest configurations.
func TestDevFlagsWithEmptyManifests(t *testing.T) {
	ctx := t.Context()

	cli := createTestClient(t)
	setupTempManifestPath(t)
	dsci := createTestDSCI()

	testCases := []struct {
		name           string
		setupDashboard func() *componentApi.Dashboard
		setupRR        func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest
		expectError    bool
		errorContains  string
		validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "WithEmptyManifestsList",
			setupDashboard: func() *componentApi.Dashboard {
				return &componentApi.Dashboard{
					Spec: componentApi.DashboardSpec{
						DashboardCommonSpec: componentApi.DashboardCommonSpec{
							DevFlagsSpec: common.DevFlagsSpec{
								DevFlags: &common.DevFlags{
									Manifests: []common.ManifestsConfig{}, // Empty manifests list
								},
							},
						},
					},
				}
			},
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboard,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.OpenDataHub},
					Manifests: []odhtypes.ManifestInfo{
						{Path: TestPath, ContextDir: ComponentName, SourcePath: "/odh"},
					},
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Manifests).Should(HaveLen(1))
				g.Expect(rr.Manifests[0].Path).Should(Equal(TestPath))
			},
		},
	}

	runDevFlagsTestCases(t, ctx, testCases)
}

// TestDevFlagsWithInvalidConfigs tests DevFlags with invalid configurations.
func TestDevFlagsWithInvalidConfigs(t *testing.T) {
	ctx := t.Context()

	cli := createTestClient(t)
	setupTempManifestPath(t)
	dsci := createTestDSCI()

	testCases := []struct {
		name           string
		setupDashboard func() *componentApi.Dashboard
		setupRR        func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest
		expectError    bool
		errorContains  string
		validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "WithInvalidURI",
			setupDashboard: func() *componentApi.Dashboard {
				return &componentApi.Dashboard{
					Spec: componentApi.DashboardSpec{
						DashboardCommonSpec: componentApi.DashboardCommonSpec{
							DevFlagsSpec: common.DevFlagsSpec{
								DevFlags: &common.DevFlags{
									Manifests: []common.ManifestsConfig{
										{
											URI: "invalid-uri", // Invalid URI
										},
									},
								},
							},
						},
					},
				}
			},
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboard,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.OpenDataHub},
					Manifests: []odhtypes.ManifestInfo{
						{Path: TestPath, ContextDir: ComponentName, SourcePath: "/odh"},
					},
				}
			},
			expectError:   true,
			errorContains: ErrorDownloadingManifests,
		},
		{
			name: "WithNilClient",
			setupDashboard: func() *componentApi.Dashboard {
				return &componentApi.Dashboard{}
			},
			setupRR: func(dashboard *componentApi.Dashboard) *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   nil, // Nil client
					Instance: dashboard,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.OpenDataHub},
					Manifests: []odhtypes.ManifestInfo{
						{Path: TestPath, ContextDir: ComponentName, SourcePath: "/odh"},
					},
				}
			},
			expectError: false,
		},
	}

	runDevFlagsTestCases(t, ctx, testCases)
}

func TestDevFlagsWithNilDevFlagsWhenDevFlagsNil(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	dashboard := &componentApi.Dashboard{
		Spec: componentApi.DashboardSpec{
			// DevFlags is nil by default
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Instance: dashboard,
	}

	err := devFlags(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}

// TestDevFlagsWithDownloadErrorWhenDownloadFails tests download error handling.
func TestDevFlagsWithDownloadErrorWhenDownloadFails(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	dashboard := &componentApi.Dashboard{
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{
						Manifests: []common.ManifestsConfig{
							{
								URI: "invalid-uri", // This should cause download error
							},
						},
					},
				},
			},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Instance: dashboard,
	}

	err := devFlags(ctx, rr)
	// Assert that download failure should return an error
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring(ErrorDownloadingManifests))
}

// TestDevFlagsSourcePathCases tests various SourcePath scenarios in a table-driven approach.
func TestDevFlagsSourcePathCases(t *testing.T) {
	ctx := t.Context()

	cli := createTestClient(t)
	setupTempManifestPath(t)
	dsci := createTestDSCI()

	testCases := []struct {
		name          string
		sourcePath    string
		expectError   bool
		errorContains string
		expectedPath  string
	}{
		{
			name:          "valid SourcePath",
			sourcePath:    TestCustomPath,
			expectError:   true, // Download will fail with real URL
			errorContains: ErrorDownloadingManifestsPrefix,
			expectedPath:  TestCustomPath,
		},
		{
			name:          "empty SourcePath",
			sourcePath:    "",
			expectError:   true, // Download will fail with real URL
			errorContains: ErrorDownloadingManifestsPrefix,
			expectedPath:  "",
		},
		{
			name:          "missing SourcePath field",
			sourcePath:    "",   // This will be set to empty string in the test
			expectError:   true, // Download will fail with real URL
			errorContains: ErrorDownloadingManifestsPrefix,
			expectedPath:  "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dashboard := &componentApi.Dashboard{
				Spec: componentApi.DashboardSpec{
					DashboardCommonSpec: componentApi.DashboardCommonSpec{
						DevFlagsSpec: common.DevFlagsSpec{
							DevFlags: &common.DevFlags{
								Manifests: []common.ManifestsConfig{
									{
										URI:        TestManifestURIInternal,
										SourcePath: tc.sourcePath,
									},
								},
							},
						},
					},
				},
			}

			rr := createTestReconciliationRequestWithManifests(
				cli, dashboard, dsci,
				common.Release{Name: cluster.OpenDataHub},
				[]odhtypes.ManifestInfo{
					{Path: TestPath, ContextDir: ComponentName, SourcePath: "/odh"},
				},
			)

			err := devFlags(ctx, rr)

			// Assert expected error behavior (download will fail with real URL)
			require.Error(t, err, "devFlags should return error for case: %s", tc.name)
			require.Contains(t, err.Error(), tc.errorContains,
				"error message should contain '%s' for case: %s", tc.errorContains, tc.name)

			// Assert dashboard instance type
			dashboard, ok := rr.Instance.(*componentApi.Dashboard)
			require.True(t, ok, "expected Instance to be *componentApi.Dashboard for case: %s", tc.name)

			// Assert manifest processing
			require.NotEmpty(t, dashboard.Spec.DevFlags.Manifests,
				"expected at least one manifest in DevFlags for case: %s", tc.name)

			// Assert SourcePath preservation in dashboard spec (before download failure)
			actualPath := dashboard.Spec.DevFlags.Manifests[0].SourcePath
			require.Equal(t, tc.expectedPath, actualPath,
				"SourcePath should be preserved as expected for case: %s", tc.name)
		})
	}
}

// TestDevFlagsWithMultipleManifestsWhenMultipleProvided tests with multiple manifests.
func TestDevFlagsWithMultipleManifestsWhenMultipleProvided(t *testing.T) {
	ctx := t.Context()

	dashboard := &componentApi.Dashboard{
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{
						Manifests: []common.ManifestsConfig{
							{
								URI:        TestManifestURIInternal,
								SourcePath: TestCustomPath,
							},
							{
								URI:        "https://example.com/manifests2.tar.gz",
								SourcePath: TestCustomPath2,
							},
						},
					},
				},
			},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Instance: dashboard,
	}

	err := devFlags(ctx, rr)

	// Assert that devFlags returns an error due to download failure
	require.Error(t, err, "devFlags should return error when using real URLs that will fail to download")
	require.Contains(t, err.Error(), ErrorDownloadingManifestsPrefix,
		"error message should contain 'error downloading manifests'")

	// Assert that the dashboard instance is preserved
	dashboard, ok := rr.Instance.(*componentApi.Dashboard)
	require.True(t, ok, "expected Instance to be *componentApi.Dashboard")

	// Assert that multiple manifests are preserved in the dashboard spec
	require.NotEmpty(t, dashboard.Spec.DevFlags.Manifests,
		"expected multiple manifests in DevFlags")
	require.Len(t, dashboard.Spec.DevFlags.Manifests, 2,
		"expected exactly 2 manifests in DevFlags")

	// Assert SourcePath preservation for both manifests
	require.Equal(t, TestCustomPath, dashboard.Spec.DevFlags.Manifests[0].SourcePath,
		"first manifest SourcePath should be preserved")
	require.Equal(t, TestCustomPath2, dashboard.Spec.DevFlags.Manifests[1].SourcePath,
		"second manifest SourcePath should be preserved")
}

// TestDevFlagsWithNilManifestsWhenManifestsNil tests with nil manifests.
func TestDevFlagsWithNilManifestsWhenManifestsNil(t *testing.T) {
	ctx := t.Context()

	dashboard := &componentApi.Dashboard{
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{
						Manifests: nil, // Nil manifests
					},
				},
			},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Instance: dashboard,
	}

	err := devFlags(ctx, rr)

	// Assert that devFlags returns no error when manifests is nil
	require.NoError(t, err, "devFlags should not return error when manifests is nil")

	// Assert that the dashboard instance is preserved
	dashboard, ok := rr.Instance.(*componentApi.Dashboard)
	require.True(t, ok, "expected Instance to be *componentApi.Dashboard")

	// Assert that nil manifests are preserved in the dashboard spec
	require.Nil(t, dashboard.Spec.DevFlags.Manifests,
		"manifests should remain nil in DevFlags")
}
