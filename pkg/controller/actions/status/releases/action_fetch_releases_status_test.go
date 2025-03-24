package releases_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFetchReleasesStatusAction(t *testing.T) {
	t.Helper()

	g := NewWithT(t)
	ctx := context.Background()

	// Root directory for temporary test files
	tempDir := filepath.Join(os.TempDir(), "releases_test")

	// Clean up created files after each test
	AfterEach(func() {
		// Remove the temporary test files
		err := os.RemoveAll(tempDir)
		Expect(err).NotTo(HaveOccurred())
	})

	// Define a test cases
	tests := []struct {
		name             string
		metadataFilePath string
		metadataContent  string
		expectedReleases int
		expectedError    bool
		devFlagsEnabled  bool
		providedStatus   []common.ComponentRelease // Provided ReleaseStatus for testing cache behavior
	}{
		{
			name:             "should successfully render releases from valid YAML",
			metadataFilePath: filepath.Join(tempDir, "valid_file.yaml"),
			metadataContent: `
releases:
  - name: Kubeflow Pipelines
    version: 2.2.0
    repoUrl: https://github.com/kubeflow/kfp-tekton
  - name: Another Component
    version: 1.3.1
    repoUrl: https://example.com/repo
`,
			expectedReleases: 2,
			expectedError:    false,
		},
		{
			name:             "should handle empty metadata file and return empty releases",
			metadataFilePath: filepath.Join(tempDir, "empty_file.yaml"),
			metadataContent:  "",
			expectedReleases: 0,
			expectedError:    false,
		},
		{
			name:             "should fail if YAML is invalid and return empty releases",
			metadataFilePath: filepath.Join(tempDir, "invalid_file.yaml"),
			metadataContent: `
releases:
  - name: Kubeflow Pipelines
    versionNumber: 2.2.0
    repoUrl: https://github.com/kubeflow/kfp-tekton
`,
			expectedReleases: 0,
			expectedError:    false,
		},
		{
			name:             "should handle empty metadata file path gracefully",
			metadataFilePath: "",
			metadataContent:  "",
			expectedReleases: 0,
			expectedError:    false,
		},
		{
			name:             "should not re-render releases if cached",
			metadataFilePath: filepath.Join(tempDir, "cached_file.yaml"),
			metadataContent: `
releases:
  - name: Kubeflow Pipelines
    version: 2.2.0
    repoUrl: https://github.com/kubeflow/kfp-tekton
`,
			expectedReleases: 1,
			expectedError:    false,
			providedStatus: []common.ComponentRelease{
				{ // Simulating cached status
					Name:    "Kubeflow Pipelines",
					Version: "0.0.0",
					RepoURL: "https://github.com/kubeflow/kfp-tekton",
				},
			},
		},
		{
			name:             "should re-render releases if DevFlags are enabled",
			metadataFilePath: filepath.Join(tempDir, "dev_flags_enabled_file.yaml"),
			metadataContent: `
releases:
  - name: Kubeflow Pipelines
    version: 2.2.0
    repoUrl: https://github.com/kubeflow/kfp-tekton
`,
			expectedReleases: 1,
			expectedError:    false,
			devFlagsEnabled:  true,
			providedStatus: []common.ComponentRelease{
				{ // Simulating cached status
					Name:    "Kubeflow Pipelines",
					Version: "0.0.0",
					RepoURL: "https://github.com/kubeflow/kfp-tekton",
				},
			},
		},
	}

	// Iterate through all test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the mock metadata file if needed
			if tt.metadataContent != "" && tt.metadataFilePath != "" {
				// Ensure the directory exists
				err := os.MkdirAll(filepath.Dir(tt.metadataFilePath), 0755)
				if err != nil {
					t.Fatalf("failed to create directories: %v", err)
				}

				// Write the test metadata content to the mock file
				err = os.WriteFile(tt.metadataFilePath, []byte(tt.metadataContent), 0600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			// Mocking DevFlags if enabled (if DevFlagsEnabled is true, simulate dev flags)
			devFlagsSpec := common.DevFlagsSpec{}
			if tt.devFlagsEnabled {
				devFlagsSpec.DevFlags = &common.DevFlags{
					Manifests: []common.ManifestsConfig{{URI: "github.com/kubeflow/kfp-tekton"}},
				}
			}

			// Create the ReconciliationRequest and set a dummy resource instance
			rr := types.ReconciliationRequest{
				Instance: &componentApi.DataSciencePipelines{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mock-instance",
					},

					// Mocking DevFlags if enabled (if DevFlagsEnabled is true, simulate dev flags)
					Spec: componentApi.DataSciencePipelinesSpec{
						DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{
							DevFlagsSpec: devFlagsSpec,
						},
					},
				},
			}

			// Check the number of componentReleases set on the instance
			withReleasesInstance, ok := rr.Instance.(common.WithReleases)
			if !ok {
				t.Fatalf("Instance does not implement WithReleases")
			}

			// Set up the action with the custom metadata file path and provided status
			action := releases.NewAction(
				releases.WithMetadataFilePath(tt.metadataFilePath),
				releases.WithComponentReleaseStatus(tt.providedStatus), // Use WithComponentReleaseStatus to set the provided status
			)

			// Run the render action
			err := action(ctx, &rr)

			// Validate results
			if tt.expectedError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			// Get release status after action
			finalReleases := withReleasesInstance.GetReleaseStatus()

			// Verify that the status is updated based on the caching and DevFlags
			if tt.providedStatus != nil {
				if tt.devFlagsEnabled {
					// DevFlags are enabled, expect re-render (new version)
					g.Expect(*finalReleases).NotTo(Equal(tt.providedStatus))
				} else {
					// Cache is available, no DevFlags, expect no re-render (cached version)
					g.Expect(*finalReleases).To(Equal(tt.providedStatus))
				}
			}

			// Validate the expected release count after action
			g.Expect(*finalReleases).To(HaveLen(tt.expectedReleases))
		})
	}
}
