package deploy_test

import (
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

// TestApplyParamsWithFallback_Integration is an integration test for ApplyParamsWithFallback.
func TestApplyParamsWithFallback_Integration(t *testing.T) {
	testCases := []struct {
		name                 string
		platform             common.Platform
		expectedOverlay      string
		expectedPlatformLine string
	}{
		{
			name:                 "OpenDataHub uses odh overlay and params.env",
			platform:             cluster.OpenDataHub,
			expectedOverlay:      "odh",
			expectedPlatformLine: "PLATFORM=odh",
		},
		{
			name:                 "SelfManagedRhoai uses rhoai overlay and params.env",
			platform:             cluster.SelfManagedRhoai,
			expectedOverlay:      "rhoai",
			expectedPlatformLine: "PLATFORM=rhoai",
		},
		{
			name:                 "ManagedRhoai uses rhoai overlay and params.env",
			platform:             cluster.ManagedRhoai,
			expectedOverlay:      "rhoai",
			expectedPlatformLine: "PLATFORM=rhoai",
		},
	}

	fs := afero.NewMemMapFs()
	componentPath := "/opt/manifests/test-component"

	_ = fs.MkdirAll(componentPath+"/base", 0755)
	_ = afero.WriteFile(fs, componentPath+"/base/params.env", []byte("PLATFORM=base\n"), 0644)
	_ = fs.MkdirAll(componentPath+"/overlays/odh", 0755)
	_ = afero.WriteFile(fs, componentPath+"/overlays/odh/params.env", []byte("PLATFORM=odh\n"), 0644)
	_ = fs.MkdirAll(componentPath+"/overlays/rhoai", 0755)
	_ = afero.WriteFile(fs, componentPath+"/overlays/rhoai/params.env", []byte("PLATFORM=rhoai\n"), 0644)

	applier := deploy.NewParamsApplier(deploy.WithFS(fs))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			overlayName := cluster.OverlayName(tc.platform)
			if overlayName != tc.expectedOverlay {
				t.Fatalf("OverlayName(platform) = %q, want %q", overlayName, tc.expectedOverlay)
			}

			paramsPath, err := applier.ApplyParamsWithFallback(componentPath, overlayName, nil)
			if err != nil {
				t.Fatalf("ApplyParamsWithFallback: %v", err)
			}

			expectedPath := componentPath + "/overlays/" + tc.expectedOverlay + "/params.env"
			if paramsPath != expectedPath {
				t.Errorf("params path = %q, want %q", paramsPath, expectedPath)
			}

			content, err := afero.ReadFile(fs, paramsPath)
			if err != nil {
				t.Fatalf("read params file: %v", err)
			}
			if !integrationContainsLine(string(content), tc.expectedPlatformLine) {
				t.Errorf("params content does not contain %q, got:\n%s", tc.expectedPlatformLine, string(content))
			}
		})
	}
}

func integrationContainsLine(content, expected string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}
