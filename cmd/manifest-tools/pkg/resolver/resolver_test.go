package resolver_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func TestReadParamsEnvKey(t *testing.T) {
	dir := t.TempDir()
	paramsFile := filepath.Join(dir, "params.env")
	content := `IMAGES_DSPO=quay.io/opendatahub/dsp-operator@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc
kube-rbac-proxy=registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9:latest
EMPTY_KEY=
`
	if err := os.WriteFile(paramsFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		key     string
		want    string
		wantErr bool
	}{
		{"digest-pinned", "IMAGES_DSPO", "quay.io/opendatahub/dsp-operator@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc", false},
		{"tagged", "kube-rbac-proxy", "registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9:latest", false},
		{"empty value", "EMPTY_KEY", "", false},
		{"missing key", "NONEXISTENT", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.ReadParamsEnvKey(paramsFile, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadParamsEnvKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ReadParamsEnvKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadParamsEnvKey_FileNotFound(t *testing.T) {
	_, err := resolver.ReadParamsEnvKey("/nonexistent/params.env", "KEY")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSplitImageRef_EdgeCases(t *testing.T) {
	tests := []struct {
		ref        string
		wantBase   string
		wantDigest string
	}{
		{"", "", ""},
		{"no-at-sign", "no-at-sign", ""},
		{"host:5000/repo@sha256:abc", "host:5000/repo", "sha256:abc"},
		{"multi@at@signs", "multi@at", "signs"},
	}

	for _, tt := range tests {
		base, digest := resolver.SplitImageRef(tt.ref)
		if base != tt.wantBase || digest != tt.wantDigest {
			t.Errorf("SplitImageRef(%q) = (%q, %q), want (%q, %q)", tt.ref, base, digest, tt.wantBase, tt.wantDigest)
		}
	}
}

func TestResolve_UnknownComponent_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	manifestsDir := filepath.Join(dir, "manifests")
	os.MkdirAll(manifestsDir, 0755)

	// Config with imageOverrides entry pointing to non-existent component
	content := `components: {}
imageOverrides:
  RELATED_IMAGE_TEST:
    component: "nonexistent-component"
    odh:
      base: "quay.io/test/image"
      tagTemplate: "v{SHA}"
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:   configFile,
		ManifestsDir: manifestsDir,
		FetchCSVImages: func(context.Context, config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
			return map[string]resolver.CSVImage{}, nil
		},
	})
	if err == nil {
		t.Error("expected error for unknown component, got nil")
	}
}

func TestResolve_ClonedCommitImageNotFound_FallsBackToCSV(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	manifestsDir := filepath.Join(dir, "manifests")
	os.MkdirAll(manifestsDir, 0755)

	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
components:
  test-component:
    odh:
      repo: "test-org/test-repo"
      ref: "main@abc123def456"
      sourcePath: "config"
imageOverrides:
  RELATED_IMAGE_TEST:
    component: "test-component"
    odh:
      base: "quay.io/test/image"
      tagTemplate: "v{SHA}"
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	fakeFetch := func(_ context.Context, _ config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
		return map[string]resolver.CSVImage{
			"RELATED_IMAGE_TEST": {Base: "quay.io/bundle/image", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		}, nil
	}

	_, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:     configFile,
		ManifestsDir:   manifestsDir,
		FetchCSVImages: fakeFetch,
	})
	if err != nil {
		t.Errorf("expected CSV fallback to succeed when SHA image not found, got error: %v", err)
	}
}

func TestResolve_UsesPlatformBuildConfigAndSkipsParamsEnvForRHOAI(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	manifestsDir := filepath.Join(dir, "manifests")
	paramsDir := filepath.Join(manifestsDir, "test-component", "base")
	if err := os.MkdirAll(paramsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paramsDir, "params.env"), []byte(
		"TEST_IMAGE=quay.io/opendatahub/from-params@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
components:
  test-component:
    odh:
      repo: opendatahub-io/test
      ref: main@1111111111111111111111111111111111111111
      sourcePath: config
    rhoai:
      repo: red-hat-data-services/test
      ref: rhoai-3.6@2222222222222222222222222222222222222222
      sourcePath: config
imageOverrides:
  RELATED_IMAGE_TEST:
    component: test-component
    paramsEnvKey: TEST_IMAGE
    odh: {}
    rhoai: {}
  RELATED_IMAGE_CSV:
    source: csv
    odh: {}
    rhoai: {}
  RELATED_IMAGE_STALE:
    source: csv
    odh:
      base: quay.io/opendatahub/stale
      digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
    rhoai:
      base: quay.io/rhoai/stale
      digest: sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	fetch := func(_ context.Context, source config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
		switch source.Repo {
		case "opendatahub-io/ODH-Build-Config":
			return map[string]resolver.CSVImage{
				"RELATED_IMAGE_TEST":  {Base: "quay.io/opendatahub/from-odh-csv", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
				"RELATED_IMAGE_CSV":   {Base: "quay.io/opendatahub/csv", Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
				"RELATED_IMAGE_STALE": {Base: "quay.io/opendatahub/stale", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
			}, nil
		case "red-hat-data-services/RHOAI-Build-Config":
			return map[string]resolver.CSVImage{
				"RELATED_IMAGE_TEST": {Base: "registry.redhat.io/rhoai/from-rhoai-csv", Digest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
				"RELATED_IMAGE_CSV":  {Base: "registry.redhat.io/rhoai/csv-rhel9", Digest: "sha256:9999999999999999999999999999999999999999999999999999999999999999"},
			}, nil
		default:
			return nil, nil
		}
	}

	_, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:     configFile,
		ManifestsDir:   manifestsDir,
		FetchCSVImages: fetch,
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	testImage := cfg.ImageOverrides["RELATED_IMAGE_TEST"]
	if testImage.ODH.Base != "quay.io/opendatahub/from-params" || testImage.ODH.Digest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("ODH did not use params.env: %#v", testImage.ODH)
	}
	if testImage.RHOAI.Base != "quay.io/rhoai/from-rhoai-csv" || testImage.RHOAI.Digest != "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" {
		t.Errorf("RHOAI did not use its normalized CSV image: %#v", testImage.RHOAI)
	}
	csvImage := cfg.ImageOverrides["RELATED_IMAGE_CSV"]
	if csvImage.ODH.Base != "quay.io/opendatahub/csv" || csvImage.RHOAI.Base != "quay.io/rhoai/csv-rhel9" {
		t.Errorf("source: csv was not resolved per platform: %#v", csvImage)
	}
	staleImage := cfg.ImageOverrides["RELATED_IMAGE_STALE"]
	if staleImage.ODH == nil || staleImage.RHOAI != nil {
		t.Errorf("stale RHOAI platform was not removed independently: %#v", staleImage)
	}
}

func TestResolve_ImportsNormalizedRHOAIImagesOnlyByDefaultAllowlist(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_EXISTING:
    source: csv
    odh:
      base: quay.io/opendatahub/existing
      digest: sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	fetch := func(_ context.Context, source config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
		if source.Repo == "opendatahub-io/ODH-Build-Config" {
			return map[string]resolver.CSVImage{
				"RELATED_IMAGE_EXISTING": {Base: "quay.io/opendatahub/existing", Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
				"RELATED_IMAGE_NEW":      {Base: "quay.io/opendatahub/new", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			}, nil
		}
		return map[string]resolver.CSVImage{
			"RELATED_IMAGE_EXISTING": {Base: "registry.redhat.io/rhoai/existing-rhel9", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
			"RELATED_IMAGE_NEW":      {Base: "registry.redhat.io/rhoai/new-rhel9", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		}, nil
	}

	_, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:          configFile,
		CSVImportRegistries: []string{"quay.io/rhoai/"},
		FetchCSVImages:      fetch,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	image := cfg.ImageOverrides["RELATED_IMAGE_NEW"]
	if image.Source != "csv" || image.ODH != nil || image.RHOAI == nil || image.RHOAI.Base != "quay.io/rhoai/new-rhel9" {
		t.Errorf("unexpected imported image: %#v", image)
	}
	existing := cfg.ImageOverrides["RELATED_IMAGE_EXISTING"]
	if existing.ODH == nil || existing.ODH.Digest != "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" ||
		existing.RHOAI == nil || existing.RHOAI.Base != "quay.io/rhoai/existing-rhel9" ||
		existing.RHOAI.Digest != "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" {
		t.Errorf("new RHOAI platform was not added to existing CSV entry: %#v", existing)
	}

	first, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:          configFile,
		CSVImportRegistries: []string{"quay.io/rhoai/"},
		FetchCSVImages:      fetch,
	}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("Resolve() is not idempotent")
	}
}

func TestResolve_ExcludesCSVImportsPerPlatform(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	relatedImagesConfigFile := filepath.Join(dir, "component-params-env.yaml")
	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_EXISTING_EXCLUDED:
    source: csv
    odh:
      base: quay.io/opendatahub/existing
      digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  RELATED_IMAGE_EXPLICIT_EXCLUDED:
    source: csv
    rhoai:
      base: quay.io/rhoai/explicit-rhel9
      digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(relatedImagesConfigFile, []byte(`rhoai_exceptions:
  - image: RELATED_IMAGE_EXISTING_EXCLUDED
    reason: ODH-only image
  - image: RELATED_IMAGE_NEW_EXCLUDED
    reason: ODH-only image
  - image: RELATED_IMAGE_EXPLICIT_EXCLUDED
    reason: Temporary exception with an explicit mapping
`), 0o644); err != nil {
		t.Fatal(err)
	}

	fetch := func(_ context.Context, source config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
		if source.Repo == "opendatahub-io/ODH-Build-Config" {
			return map[string]resolver.CSVImage{
				"RELATED_IMAGE_EXISTING_EXCLUDED": {Base: "quay.io/opendatahub/existing", Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
			}, nil
		}
		return map[string]resolver.CSVImage{
			"RELATED_IMAGE_EXISTING_EXCLUDED": {Base: "registry.redhat.io/rhoai/existing-rhel9", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
			"RELATED_IMAGE_NEW_EXCLUDED":      {Base: "registry.redhat.io/rhoai/new-rhel9", Digest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
			"RELATED_IMAGE_EXPLICIT_EXCLUDED": {Base: "registry.redhat.io/rhoai/explicit-rhel9", Digest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		}, nil
	}

	if _, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:              configFile,
		RelatedImagesConfigFile: relatedImagesConfigFile,
		CSVImportRegistries:     []string{"quay.io/rhoai/"},
		FetchCSVImages:          fetch,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	existing := cfg.ImageOverrides["RELATED_IMAGE_EXISTING_EXCLUDED"]
	if existing.ODH == nil || existing.ODH.Digest != "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" || existing.RHOAI != nil {
		t.Errorf("excluded RHOAI platform was added while ODH was preserved: %#v", existing)
	}
	if _, exists := cfg.ImageOverrides["RELATED_IMAGE_NEW_EXCLUDED"]; exists {
		t.Error("excluded RHOAI CSV image was imported")
	}
	explicit := cfg.ImageOverrides["RELATED_IMAGE_EXPLICIT_EXCLUDED"]
	if explicit.RHOAI == nil || explicit.RHOAI.Digest != "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" {
		t.Errorf("explicit RHOAI mapping was not refreshed: %#v", explicit)
	}
}

func TestResolve_DoesNotCrossFallbackWhenRHOAICSVFetchFails(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides: {}
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	fetch := func(_ context.Context, source config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
		if source.Repo == "red-hat-data-services/RHOAI-Build-Config" {
			return nil, os.ErrNotExist
		}
		return map[string]resolver.CSVImage{}, nil
	}
	_, err := resolver.Resolve(t.Context(), resolver.Options{ConfigFile: configFile, FetchCSVImages: fetch})
	if err == nil || !strings.Contains(err.Error(), "rhoai Build-Config CSV") {
		t.Fatalf("Resolve() error = %v, want RHOAI fetch error", err)
	}
}

// movingTagGuardFixture writes a config whose only image override resolves a
// literal moving tag, with an already-pinned digest to compare against.
// componentRef selects the component ref written into the fixture; guard
// tests pass a pinned ref, the unpinned-SHA test passes "main".
func movingTagGuardFixture(t *testing.T, componentRef string) (configFile string, pinnedDigest string) {
	dir := t.TempDir()
	configFile = filepath.Join(dir, "manifests-config.yaml")
	manifestsDir := filepath.Join(dir, "manifests")
	os.MkdirAll(manifestsDir, 0755)
	pinnedDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
components:
  test-component:
    odh:
      repo: "test-org/test-repo"
      ref: "` + componentRef + `"
      sourcePath: "config"
imageOverrides:
  RELATED_IMAGE_TEST:
    component: "test-component"
    tagTemplate: "odh-stable"
    odh:
      base: "quay.io/test/image"
      digest: "` + pinnedDigest + `"
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return configFile, pinnedDigest
}

// TestResolve_LiteralTagWithoutSHA Verifies that a literal tag template still
// resolves via registry lookup when the component ref carries no commit SHA
// (e.g. an unpinned "main" ref).
func TestResolve_LiteralTagWithoutSHA(t *testing.T) {
	configFile, _ := movingTagGuardFixture(t, "main")

	newerDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	fakeDigest := func(_ context.Context, imageRef string) (string, error) {
		if imageRef != "quay.io/test/image:odh-stable" {
			return "", fmt.Errorf("unexpected image ref %s", imageRef)
		}
		return newerDigest, nil
	}
	fakeLabels := func(_ context.Context, _ string) (map[string]string, error) {
		// Candidate is newer than the pinned image; guard must allow it.
		return map[string]string{"release": "300"}, nil
	}

	results, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:       configFile,
		ResolveDigest:    fakeDigest,
		FetchImageLabels: fakeLabels,
		FetchCSVImages: func(_ context.Context, _ config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
			return map[string]resolver.CSVImage{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	found := false
	for _, r := range results {
		if r.EnvName == "RELATED_IMAGE_TEST" && r.Digest == newerDigest && r.Source == "literal-tag" {
			found = true
		}
	}
	if !found {
		t.Fatalf("literal tag did not resolve without a commit SHA; results: %+v", results)
	}
}

// TestResolve_LiteralTagFailedLookupKeepsPinnedDigest Verifies that when the
// moving-tag registry lookup fails, the resolver keeps the accepted pinned
// digest instead of falling back to a params.env or CSV digest, which is not
// guarded against regressions.
func TestResolve_LiteralTagFailedLookupKeepsPinnedDigest(t *testing.T) {
	configFile, pinnedDigest := movingTagGuardFixture(t, "main@abc123def456")

	fakeDigest := func(_ context.Context, _ string) (string, error) {
		return "", fmt.Errorf("registry unavailable")
	}
	csvDigest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	fakeLabels := func(_ context.Context, _ string) (map[string]string, error) {
		return nil, fmt.Errorf("registry unavailable")
	}

	results, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:       configFile,
		ResolveDigest:    fakeDigest,
		FetchImageLabels: fakeLabels,
		FetchCSVImages: func(_ context.Context, _ config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
			return map[string]resolver.CSVImage{
				"RELATED_IMAGE_TEST": {Base: "quay.io/test/image", Digest: csvDigest},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	for _, r := range results {
		if r.EnvName == "RELATED_IMAGE_TEST" && r.Digest == csvDigest && r.Source == "csv" {
			t.Fatalf("failed moving-tag lookup fell back to unguarded CSV digest %s", csvDigest)
		}
	}

	// The config on disk must keep the accepted pinned digest.
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), csvDigest) {
		t.Fatalf("config was rewritten with the unguarded CSV digest %s", csvDigest)
	}
	if !strings.Contains(string(data), pinnedDigest) {
		t.Fatalf("config lost the pinned digest %s", pinnedDigest)
	}
}

func TestResolve_MovingTagRejectsOlderSource(t *testing.T) {
	configFile, pinnedDigest := movingTagGuardFixture(t, "main@abc123def456")
	newerDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	fakeDigest := func(_ context.Context, _ string) (string, error) {
		return newerDigest, nil
	}
	fakeLabels := func(_ context.Context, imageRef string) (map[string]string, error) {
		// Candidate is older than the pinned image.
		if strings.Contains(imageRef, newerDigest) {
			return map[string]string{"release": "100"}, nil
		}
		return map[string]string{"release": "200"}, nil
	}

	results, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:       configFile,
		ResolveDigest:    fakeDigest,
		FetchImageLabels: fakeLabels,
		FetchCSVImages: func(_ context.Context, _ config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
			return map[string]resolver.CSVImage{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	for _, r := range results {
		if r.EnvName == "RELATED_IMAGE_TEST" && r.Digest == newerDigest {
			t.Fatalf("resolver pinned older moving-tag digest %s over %s", newerDigest, pinnedDigest)
		}
	}

	// The config on disk must keep the pinned digest.
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), newerDigest) {
		t.Fatalf("config was rewritten with the regressed digest %s", newerDigest)
	}
	if !strings.Contains(string(data), pinnedDigest) {
		t.Fatalf("config lost the pinned digest %s", pinnedDigest)
	}
}

func TestResolve_MovingTagAcceptsNewerSource(t *testing.T) {
	configFile, _ := movingTagGuardFixture(t, "main@abc123def456")
	newerDigest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	fakeDigest := func(_ context.Context, _ string) (string, error) {
		return newerDigest, nil
	}
	fakeLabels := func(_ context.Context, imageRef string) (map[string]string, error) {
		if strings.Contains(imageRef, newerDigest) {
			return map[string]string{"release": "300"}, nil
		}
		return map[string]string{"release": "200"}, nil
	}

	results, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:       configFile,
		ResolveDigest:    fakeDigest,
		FetchImageLabels: fakeLabels,
		FetchCSVImages: func(_ context.Context, _ config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
			return map[string]resolver.CSVImage{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	found := false
	for _, r := range results {
		if r.EnvName == "RELATED_IMAGE_TEST" && r.Digest == newerDigest && r.Source == "literal-tag" {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolver did not pin newer moving-tag digest %s; results: %+v", newerDigest, results)
	}
}

func TestResolve_MovingTagFailsOpenWhenLabelsUnavailable(t *testing.T) {
	configFile, _ := movingTagGuardFixture(t, "main@abc123def456")
	newerDigest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	fakeDigest := func(_ context.Context, _ string) (string, error) {
		return newerDigest, nil
	}
	fakeLabels := func(_ context.Context, _ string) (map[string]string, error) {
		return nil, fmt.Errorf("registry unavailable")
	}

	results, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:       configFile,
		ResolveDigest:    fakeDigest,
		FetchImageLabels: fakeLabels,
		FetchCSVImages: func(_ context.Context, _ config.BuildConfigRepo) (map[string]resolver.CSVImage, error) {
			return map[string]resolver.CSVImage{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	found := false
	for _, r := range results {
		if r.EnvName == "RELATED_IMAGE_TEST" && r.Digest == newerDigest {
			found = true
		}
	}
	if !found {
		t.Fatalf("guard did not fail open when labels unavailable; results: %+v", results)
	}
}
