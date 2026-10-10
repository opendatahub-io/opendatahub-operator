package resolver_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

const catalogYAML = `
components:
  ray:
    odh:
      repo: opendatahub-io/kuberay
      ref: dev@ad425f7febc4039f2378747f2a0ea5dcf5a2263f
      sourcePath: config
ccmCharts:
  cert-manager-operator:
    odh:
      repo: opendatahub-io/odh-gitops
      ref: main@abc1234
      sourcePath: charts
componentCharts:
  dashboard-operator:
    odh:
      repo: opendatahub-io/odh-dashboard
      ref: main@abc1234
      sourcePath: charts
`

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func writePlantedConfig(t *testing.T, overridesYAML string) (string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifests-config.yaml")
	content := []byte(strings.TrimSpace(catalogYAML) + "\nimageOverrides:\n" + overridesYAML)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	return path, content
}

func writeParamsTree(t *testing.T, component, paramsEnv string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, component, "base")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "params.env"), []byte(paramsEnv), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolve_MalformedImageOverrides(t *testing.T) {
	rayBase := `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    component: ray
    tagTemplate: %s
    odh:
      base: "quay.io/opendatahub/kuberay-operator"
`

	tests := []struct {
		name       string
		overrides  string
		manifests  string
		wantSubstr []string
	}{
		{
			name: "unknown component",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    component: does-not-exist
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.component", "does-not-exist"},
		},
		{
			name: "csv unknown component",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    source: csv
    component: does-not-exist
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.component", "does-not-exist"},
		},
		{
			name:       "unknown placeholder",
			overrides:  sprintfOverride(rayBase, `"{FOO}"`),
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.tagTemplate", "{FOO}"},
		},
		{
			name:       "lowercase placeholder",
			overrides:  sprintfOverride(rayBase, `"{sha}"`),
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.tagTemplate", "{sha}"},
		},
		{
			name:       "no braces",
			overrides:  sprintfOverride(rayBase, `"SHA"`),
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.tagTemplate", "SHA"},
		},
		{
			name:       "whitespace tagTemplate",
			overrides:  sprintfOverride(rayBase, `"   "`),
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.tagTemplate"},
		},
		{
			name: "tagTemplate without component",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    tagTemplate: "{SHA}"
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.tagTemplate", "requires component"},
		},
		{
			name: "csv tagTemplate without component",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    source: csv
    tagTemplate: "{SHA}"
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.tagTemplate", "requires component"},
		},
		{
			name: "paramsEnvKey missing key",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    component: ray
    paramsEnvKey: missing-key
`,
			manifests:  "tree",
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.paramsEnvKey", "missing-key"},
		},
		{
			name: "paramsEnvKey empty ManifestsDir",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    component: ray
    paramsEnvKey: missing-key
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.paramsEnvKey", "manifests directory is required", "missing-key"},
		},
		{
			name: "paramsEnvKey requires component",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    paramsEnvKey: IMAGES_DSPO
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.paramsEnvKey", "requires component"},
		},
		{
			name: "csv paramsEnvKey requires component",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    source: csv
    paramsEnvKey: IMAGES_DSPO
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.paramsEnvKey", "requires component"},
		},
		{
			name: "prefix",
			overrides: `
  NOT_A_RELATED_IMAGE:
    component: ray
`,
			wantSubstr: []string{"imageOverrides.NOT_A_RELATED_IMAGE", "RELATED_IMAGE_"},
		},
		{
			name: "source csvv",
			overrides: `
  RELATED_IMAGE_ODH_RAY_IMAGE:
    source: csvv
    component: ray
`,
			wantSubstr: []string{"imageOverrides.RELATED_IMAGE_ODH_RAY_IMAGE.source", "csvv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, original := writePlantedConfig(t, tt.overrides)
			opts := resolver.Options{ConfigFile: path}
			switch tt.manifests {
			case "tree":
				opts.ManifestsDir = writeParamsTree(t, "ray", "OTHER_KEY=value\n")
			case "empty":
				opts.ManifestsDir = t.TempDir()
			}

			_, err := resolver.Resolve(cancelledContext(), opts)
			if err == nil {
				t.Fatal("expected Resolve to fail")
			}
			msg := err.Error()
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(msg, sub) {
					t.Errorf("error %q, want substring %q", msg, sub)
				}
			}

			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, original) {
				t.Error("Resolve wrote manifests-config.yaml on error")
			}
		})
	}
}

func sprintfOverride(format, tag string) string {
	return strings.Replace(format, "%s", tag, 1)
}

func TestEnvVarQuotedInOperatorGo(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"internal/prod.go":             `env := "RELATED_IMAGE_ODH_PROD"`,
		"internal/prod_test.go":        `env := "RELATED_IMAGE_ODH_TESTFILE"`,
		"cmd/manifest-tools/ignore.go": `env := "RELATED_IMAGE_ODH_TOOLS"`,
		"cmd/other/main.go":            `env := "RELATED_IMAGE_ODH_CMD"`,
		"pkg/deploy/env.go":            `env := "RELATED_IMAGE_ODH_PKG"`,
		"pkg/manifest-tools/keep.go":   `env := "RELATED_IMAGE_ODH_NESTED"`,
	}
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "internal production Go", env: "RELATED_IMAGE_ODH_PROD", want: true},
		{name: "cmd production Go", env: "RELATED_IMAGE_ODH_CMD", want: true},
		{name: "pkg production Go", env: "RELATED_IMAGE_ODH_PKG", want: true},
		{name: "pkg directory named manifest-tools", env: "RELATED_IMAGE_ODH_NESTED", want: true},
		{name: "test file", env: "RELATED_IMAGE_ODH_TESTFILE"},
		{name: "cmd/manifest-tools", env: "RELATED_IMAGE_ODH_TOOLS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := envVarQuotedInOperatorGo(root, tt.env)
			if err != nil {
				t.Fatalf("%s: %v", tt.env, err)
			}
			if found != tt.want {
				t.Errorf("envVarQuotedInOperatorGo(%q) = %v, want %v", tt.env, found, tt.want)
			}
		})
	}
}

func envVarQuotedInOperatorGo(repoRoot, envName string) (bool, error) {
	needle := []byte(strconv.Quote(envName))
	for _, rel := range []string{"internal", "pkg", "cmd"} {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		var found bool
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				rel, relErr := filepath.Rel(repoRoot, path)
				if relErr == nil && rel == filepath.Join("cmd", "manifest-tools") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if bytes.Contains(data, needle) {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if found {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
}
