package resolver_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func TestValidateProvenance(t *testing.T) {
	const componentRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const sourceRevision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	tests := []struct {
		name          string
		policy        string
		imageRevision string
		sourceFound   bool
		source        string
		wantErr       string
	}{
		{name: "component revision matches", policy: "expectedRevision: component", imageRevision: componentRevision},
		{name: "component revision mismatch", policy: "expectedRevision: component", imageRevision: sourceRevision, wantErr: "does not match expected revision"},
		{name: "source revision matches", policy: "sourceRevisionPath: manifests/.source.yaml", imageRevision: sourceRevision, sourceFound: true, source: sourceRevision},
		{name: "source revision mismatch", policy: "sourceRevisionPath: manifests/.source.yaml", imageRevision: componentRevision, sourceFound: true, source: sourceRevision, wantErr: "does not match expected revision"},
		{name: "optional source absent", policy: "sourceRevisionPath: manifests/.source.yaml", imageRevision: componentRevision},
		{name: "missing OCI revision label", policy: "expectedRevision: component"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configFile := filepath.Join(dir, "manifests-config.yaml")
			content := `components:
  test-component:
    odh:
      repo: test-org/test-repo
      ref: main@` + componentRevision + `
      sourcePath: config
imageOverrides:
  RELATED_IMAGE_TEST:
    component: test-component
    source: csv
    odh:
      base: quay.io/test/image
      digest: ` + digest + `
      ` + tt.policy + `
`
			if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			err := resolver.ValidateProvenance(t.Context(), resolver.ProvenanceOptions{
				ConfigFile: configFile,
				ResolveRegistryImage: func(_ context.Context, ref string) (resolver.RegistryImage, error) {
					if !strings.Contains(ref, digest) {
						t.Fatalf("registry ref %q does not contain digest", ref)
					}
					return resolver.RegistryImage{Digest: digest, Revision: tt.imageRevision}, nil
				},
				FetchSourceRevision: func(_ context.Context, _ config.PlatformRepo, path string) (string, bool, error) {
					if path != "manifests/.source.yaml" {
						t.Fatalf("source path = %q", path)
					}
					return tt.source, tt.sourceFound, nil
				},
			})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("ValidateProvenance() error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("ValidateProvenance() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
