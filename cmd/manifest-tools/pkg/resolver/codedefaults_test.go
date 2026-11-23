package resolver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

const syncCodeDefaultsConfig = `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE:
    source: csv
    odh:
      base: quay.io/opendatahub/odh-kube-auth-proxy
      digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`

const syncCodeDefaultsGoSource = `package gateway

func getKubeAuthProxyImage() string {
	if image := os.Getenv("RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"); image != "" {
		return image
	}
	// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
	return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
}
`

func writeSyncCodeDefaultsTree(t *testing.T, goContent string) (configFile, root string) {
	t.Helper()
	dir := t.TempDir()
	configFile = filepath.Join(dir, "manifests-config.yaml")
	if err := os.WriteFile(configFile, []byte(syncCodeDefaultsConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(dir, "internal", "controller", "services", "gateway")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway_support.go"), []byte(goContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return configFile, root
}

func readGoFile(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "gateway_support.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// syncCodeDefaults loads configFile and runs the collect+apply flow with
// no save in between; the Resolve-level tests cover the interleaved save.
func syncCodeDefaults(t *testing.T, configFile, root string) (bool, error) {
	t.Helper()
	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	updates, err := collectCodeDefaults(cfg, root)
	if err != nil {
		return false, err
	}
	return applyCodeDefaultUpdates(updates)
}

func TestSyncCodeDefaults_UpdatesStaleLiteral(t *testing.T) {
	configFile, root := writeSyncCodeDefaultsTree(t, syncCodeDefaultsGoSource)

	changed, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if !changed {
		t.Error("SyncCodeDefaults() changed = false, want true for stale literal")
	}

	got := readGoFile(t, root)
	want := "quay.io/opendatahub/odh-kube-auth-proxy@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if !strings.Contains(got, `"`+want+`"`) {
		t.Errorf("gateway_support.go was not rewritten with %q:\n%s", want, got)
	}
}

func TestSyncCodeDefaults_IdempotentWhenInSync(t *testing.T) {
	configFile, root := writeSyncCodeDefaultsTree(t, syncCodeDefaultsGoSource)

	// First pass rewrites the stale literal.
	if _, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile)); err != nil {
		t.Fatalf("first SyncCodeDefaults() error = %v", err)
	}
	first := readGoFile(t, root)

	// Second pass must report no change and leave the file byte-identical.
	changed, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err != nil {
		t.Fatalf("second SyncCodeDefaults() error = %v", err)
	}
	if changed {
		t.Error("SyncCodeDefaults() changed = true on already-synced file, want false")
	}
	if second := readGoFile(t, root); first != second {
		t.Error("SyncCodeDefaults() is not idempotent")
	}
}

func TestSyncCodeDefaults_TrailingCommentMarkerIgnored(t *testing.T) {
	// Marker text in a trailing comment must not be treated as a marker:
	// findCodeDefaultSites only pairs markers that start a line, so this
	// file carries no marker at all.
	configFile, root := writeSyncCodeDefaultsTree(t, `package gateway

func tagged() string {
	x := 1 // sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
	return "unrelated"
}
`)

	changed, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if changed {
		t.Error("SyncCodeDefaults() changed = true for trailing-comment marker, want false")
	}
	if got := readGoFile(t, root); strings.Contains(got, "aaaa") {
		t.Errorf("trailing-comment marker rewrote an unrelated literal:\n%s", got)
	}
}

func TestSyncCodeDefaults_NoMarkersIsNoOp(t *testing.T) {
	configFile, _ := writeSyncCodeDefaultsTree(t, `package gateway

func unrelated() string {
	return "no marker here"
}
`)

	changed, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if changed {
		t.Error("SyncCodeDefaults() changed = true without markers, want false")
	}
}

func TestSyncCodeDefaults_UnknownEnvNameFails(t *testing.T) {
	configFile, _ := writeSyncCodeDefaultsTree(t, `package gateway

// sync-digests: RELATED_IMAGE_NOT_IN_CONFIG
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`)

	_, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err == nil {
		t.Fatal("expected error for marker naming an env var missing from imageOverrides, got nil")
	}
	if !strings.Contains(err.Error(), "RELATED_IMAGE_NOT_IN_CONFIG") {
		t.Errorf("error should name the unknown env var, got: %v", err)
	}
}

func TestSyncCodeDefaults_MarkerWithoutLiteralFails(t *testing.T) {
	configFile, _ := writeSyncCodeDefaultsTree(t, `package gateway

// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
func noLiteral() {}
`)

	_, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err == nil {
		t.Fatal("expected error for marker without a following string literal, got nil")
	}
}

func TestSyncCodeDefaults_SkipsTestFilesAndVendor(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	if err := os.WriteFile(configFile, []byte(syncCodeDefaultsConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	stale := `// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`
	testFile := filepath.Join(dir, "pkg", "gateway")
	if err := os.MkdirAll(testFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testFile, "gateway_support_test.go"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	vendorFile := filepath.Join(dir, "vendor", "example.com", "lib")
	if err := os.MkdirAll(vendorFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorFile, "lib.go"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := syncCodeDefaults(t, configFile, dir)
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if changed {
		t.Error("SyncCodeDefaults() changed = true, want false when only _test.go and vendor files carry markers")
	}
}

func TestSyncCodeDefaults_QuotedCommentBetweenMarkerAndLiteral(t *testing.T) {
	// Quoted text in a comment between the marker and the literal must
	// not be mistaken for the pinned literal.
	configFile, root := writeSyncCodeDefaultsTree(t, `package gateway

// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
// The pinned "image" below must stay a digest ref; see "docs".
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`)

	changed, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if !changed {
		t.Error("SyncCodeDefaults() changed = false, want true for stale literal")
	}

	got := readGoFile(t, root)
	if !strings.Contains(got, `The pinned "image" below must stay a digest ref; see "docs".`) {
		t.Errorf("comment between marker and literal was modified:\n%s", got)
	}
	want := `"quay.io/opendatahub/odh-kube-auth-proxy@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`
	if !strings.Contains(got, want) {
		t.Errorf("return literal was not rewritten to %s:\n%s", want, got)
	}
}

func TestApplyCodeDefaultUpdates_ConcurrentEditAbortsWrite(t *testing.T) {
	configFile, root := writeSyncCodeDefaultsTree(t, syncCodeDefaultsGoSource)
	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	updates, err := collectCodeDefaults(cfg, filepath.Dir(configFile))
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a concurrent edit between collection and application.
	goFile := filepath.Join(root, "gateway_support.go")
	touched := syncCodeDefaultsGoSource + "// touched by a concurrent edit\n"
	if err := os.WriteFile(goFile, []byte(touched), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := applyCodeDefaultUpdates(updates)
	if err == nil {
		t.Fatal("expected error for file changed during sync, got nil")
	}
	if changed {
		t.Error("applyCodeDefaultUpdates() changed = true, want false when a file changed mid-sync")
	}
	if got := readGoFile(t, root); got != touched {
		t.Errorf("concurrently edited file was overwritten:\n%s", got)
	}
}

func TestSyncCodeDefaults_SkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	if err := os.WriteFile(configFile, []byte(syncCodeDefaultsConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "target.go")
	if err := os.WriteFile(target, []byte(`// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	changed, err := syncCodeDefaults(t, configFile, dir)
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if changed {
		t.Error("SyncCodeDefaults() changed = true, want false when only a symlink carries a marker")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "aaaa") {
		t.Errorf("symlink target was rewritten through the link:\n%s", data)
	}
}

func TestResolve_CodeDefaultValidationErrorSkipsConfigWrite(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	gatewayDir := filepath.Join(dir, "internal", "controller", "services", "gateway")
	if err := os.MkdirAll(gatewayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goFile := filepath.Join(gatewayDir, "gateway_support.go")

	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE:
    source: csv
    odh:
      base: quay.io/opendatahub/odh-kube-auth-proxy
      digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Marker names an env var missing from imageOverrides.
	if err := os.WriteFile(goFile, []byte(`package gateway

// sync-digests: RELATED_IMAGE_NOT_IN_CONFIG
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	fetch := func(_ context.Context, _ config.BuildConfigRepo) (map[string]CSVImage, error) {
		return map[string]CSVImage{}, nil
	}

	results, err := Resolve(t.Context(), Options{
		ConfigFile:     configFile,
		FetchCSVImages: fetch,
	})
	if err == nil {
		t.Fatal("expected error for marker naming an env var missing from imageOverrides, got nil")
	}
	// The failure must come from marker validation, not from anything
	// earlier in the Resolve flow.
	if !strings.Contains(err.Error(), "RELATED_IMAGE_NOT_IN_CONFIG") || !strings.Contains(err.Error(), "not found in imageOverrides") {
		t.Errorf("error should report RELATED_IMAGE_NOT_IN_CONFIG as missing from imageOverrides, got: %v", err)
	}
	if results != nil {
		t.Errorf("Resolve() returned results %v alongside an error, want nil", results)
	}

	// The config must not have been rewritten: validation runs before Save.
	after, readErr := os.ReadFile(configFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != content {
		t.Errorf("manifests-config.yaml was rewritten despite a code default validation error:\n%s", after)
	}
}

func TestResolve_SyncsCodeDefaultAfterDigestUpdate(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	gatewayDir := filepath.Join(dir, "internal", "controller", "services", "gateway")
	if err := os.MkdirAll(gatewayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goFile := filepath.Join(gatewayDir, "gateway_support.go")

	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE:
    source: csv
    odh:
      base: quay.io/opendatahub/odh-kube-auth-proxy
      digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goFile, []byte(syncCodeDefaultsGoSource), 0o644); err != nil {
		t.Fatal(err)
	}

	freshDigest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	fetch := func(_ context.Context, _ config.BuildConfigRepo) (map[string]CSVImage, error) {
		return map[string]CSVImage{
			"RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE": {Base: "quay.io/opendatahub/odh-kube-auth-proxy", Digest: freshDigest},
		}, nil
	}

	if _, err := Resolve(t.Context(), Options{
		ConfigFile:     configFile,
		FetchCSVImages: fetch,
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	data, err := os.ReadFile(goFile)
	if err != nil {
		t.Fatal(err)
	}
	want := `"quay.io/opendatahub/odh-kube-auth-proxy@` + freshDigest + `"`
	if !strings.Contains(string(data), want) {
		t.Errorf("Resolve() did not sync the code default literal to %s:\n%s", want, data)
	}

	// The persisted configuration must carry the same freshly resolved
	// digest the Go literal was rewritten with. Reload and compare the
	// typed ODH entry: a whole-file substring check could pass against
	// another platform's identical digest.
	savedCfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	savedOverride, ok := savedCfg.ImageOverrides["RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"]
	if !ok {
		t.Fatal("saved config lost the RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE entry")
	}
	savedImg := savedOverride.PlatformImage("odh")
	if savedImg == nil {
		t.Fatal("saved config has no ODH platform image for RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE")
	}
	if savedImg.Digest != freshDigest {
		t.Errorf("saved ODH digest = %s, want %s", savedImg.Digest, freshDigest)
	}
	if savedImg.Base != "quay.io/opendatahub/odh-kube-auth-proxy" {
		t.Errorf("saved ODH base = %s, want quay.io/opendatahub/odh-kube-auth-proxy", savedImg.Base)
	}
}

func TestSyncCodeDefaults_MixedCaseMarkerNameFails(t *testing.T) {
	// The full marker field must be validated: RELATED_IMAGE_PROXY_amd64
	// must be rejected instead of being resolved through its
	// RELATED_IMAGE_PROXY prefix.
	configFile, root := writeSyncCodeDefaultsTree(t, `package gateway

// sync-digests: RELATED_IMAGE_PROXY_amd64
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`)

	_, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err == nil {
		t.Fatal("expected error for marker with a non-uppercase env name, got nil")
	}
	if !strings.Contains(err.Error(), "RELATED_IMAGE_PROXY_amd64") {
		t.Errorf("error should name the rejected marker field, got: %v", err)
	}
	if got := readGoFile(t, root); strings.Contains(got, "aaaa") {
		t.Errorf("file with a rejected marker was rewritten:\n%s", got)
	}
}

func TestSyncCodeDefaults_OutOfScopeLiteralNotPaired(t *testing.T) {
	// A marker whose statement ends without a string literal must fail:
	// the digest-pinned literal in the later declaration belongs to a
	// different statement and must never be paired with the marker.
	configFile, root := writeSyncCodeDefaultsTree(t, `package gateway

func getKubeAuthProxyImage() string {
	if image := os.Getenv("RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"); image != "" {
		return image
	}
	// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE
	return fallbackImage()
}

var fallback = "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`)

	_, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err == nil {
		t.Fatal("expected error for marker without an in-statement string literal, got nil")
	}
	if !strings.Contains(err.Error(), "marker without a string literal to rewrite") {
		t.Errorf("error should report the unpaired marker, got: %v", err)
	}
	if got := readGoFile(t, root); strings.Contains(got, "aaaa") {
		t.Errorf("out-of-scope literal was rewritten through the marker:\n%s", got)
	}
}

func TestSyncCodeDefaults_StandaloneMarkerFails(t *testing.T) {
	// A sync-digests: directive with no env name field must be rejected:
	// treating the file as unmarked would silently let the default
	// drift.
	configFile, root := writeSyncCodeDefaultsTree(t, `package gateway

// sync-digests:
return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`)

	_, err := syncCodeDefaults(t, configFile, filepath.Dir(configFile))
	if err == nil {
		t.Fatal("expected error for a standalone sync-digests: directive, got nil")
	}
	if !strings.Contains(err.Error(), "without a RELATED_IMAGE_* env var name") {
		t.Errorf("error should report the missing env var name, got: %v", err)
	}
	if got := readGoFile(t, root); strings.Contains(got, "aaaa") {
		t.Errorf("file with a standalone marker was rewritten:\n%s", got)
	}
}

func TestSyncCodeDefaults_SymlinkedRootFails(t *testing.T) {
	configFile, root := writeSyncCodeDefaultsTree(t, syncCodeDefaultsGoSource)
	link := filepath.Join(filepath.Dir(configFile), "linked-root")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// The trailing-separator variant matters: Lstat on "link/" resolves
	// the symlink and reports its target, which would let the root pass
	// the symlink check.
	for _, rootArg := range []string{link, link + string(os.PathSeparator)} {
		_, err := syncCodeDefaults(t, configFile, rootArg)
		if err == nil {
			t.Errorf("expected error for symlinked scan root %s, got nil", rootArg)
			continue
		}
		if !strings.Contains(err.Error(), "is a symlink") {
			t.Errorf("error for %s should report the symlinked root, got: %v", rootArg, err)
		}
	}
	if got := readGoFile(t, root); strings.Contains(got, "aaaa") {
		t.Errorf("file under the symlinked root was rewritten:\n%s", got)
	}
}

func TestSyncCodeDefaults_RealDirWithTrailingSeparatorAccepted(t *testing.T) {
	configFile, root := writeSyncCodeDefaultsTree(t, syncCodeDefaultsGoSource)

	changed, err := syncCodeDefaults(t, configFile, root+string(os.PathSeparator))
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if !changed {
		t.Error("SyncCodeDefaults() changed = false, want true for a real directory root with a trailing separator")
	}
	if got := readGoFile(t, root); !strings.Contains(got, "aaaa") {
		t.Errorf("file under the real root was not rewritten:\n%s", got)
	}
}

func TestSyncCodeDefaults_EmptyBaseComponentFails(t *testing.T) {
	// name.NewDigest's repository check is a character class and accepts
	// repeated slashes; the empty component must be caught explicitly.
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	if err := os.WriteFile(configFile, []byte(`buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE:
    source: csv
    odh:
      base: quay.io/example//proxy
      digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "internal", "controller", "services", "gateway")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway_support.go"), []byte(syncCodeDefaultsGoSource), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := syncCodeDefaults(t, configFile, dir)
	if err == nil {
		t.Fatal("expected error for a base with an empty path component, got nil")
	}
	if !strings.Contains(err.Error(), "empty path component") {
		t.Errorf("error should report the empty path component, got: %v", err)
	}
	if got := readGoFile(t, root); !strings.Contains(got, "bbbb") {
		t.Errorf("stale literal was modified despite the malformed base:\n%s", got)
	}
}

func TestSyncCodeDefaults_RegistryPortAndNestedPathAccepted(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	if err := os.WriteFile(configFile, []byte(`buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE:
    source: csv
    odh:
      base: localhost:5000/example/nested/proxy
      digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "internal", "controller", "services", "gateway")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway_support.go"), []byte(syncCodeDefaultsGoSource), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := syncCodeDefaults(t, configFile, dir)
	if err != nil {
		t.Fatalf("SyncCodeDefaults() error = %v", err)
	}
	if !changed {
		t.Error("SyncCodeDefaults() changed = false, want true for a registry-port nested base")
	}
	want := `"localhost:5000/example/nested/proxy@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`
	if got := readGoFile(t, root); !strings.Contains(got, want) {
		t.Errorf("literal was not rewritten to %s:\n%s", want, got)
	}
}

func TestSyncCodeDefaults_NonDirectoryRootFails(t *testing.T) {
	configFile, _ := writeSyncCodeDefaultsTree(t, syncCodeDefaultsGoSource)

	_, err := syncCodeDefaults(t, configFile, configFile)
	if err == nil {
		t.Fatal("expected error for a non-directory scan root, got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error should report the non-directory root, got: %v", err)
	}
}

func TestResolve_MalformedCSVBaseLeavesFilesUnchanged(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	gatewayDir := filepath.Join(dir, "internal", "controller", "services", "gateway")
	if err := os.MkdirAll(gatewayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goFile := filepath.Join(gatewayDir, "gateway_support.go")

	content := `buildConfig:
  odh:
    repo: opendatahub-io/ODH-Build-Config
    ref: main@1111111111111111111111111111111111111111
  rhoai:
    repo: red-hat-data-services/RHOAI-Build-Config
    ref: rhoai-3.6@2222222222222222222222222222222222222222
imageOverrides:
  RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE:
    source: csv
    odh:
      base: quay.io/opendatahub/odh-kube-auth-proxy
      digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goFile, []byte(syncCodeDefaultsGoSource), 0o644); err != nil {
		t.Fatal(err)
	}

	// The CSV returns an uppercase base: well-shaped enough for the regex
	// but rejected by the container name parser.
	fetch := func(_ context.Context, _ config.BuildConfigRepo) (map[string]CSVImage, error) {
		return map[string]CSVImage{
			"RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE": {Base: "quay.io/opendatahub/ODH_KUBE_AUTH_PROXY", Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		}, nil
	}

	if _, err := Resolve(t.Context(), Options{
		ConfigFile:     configFile,
		FetchCSVImages: fetch,
	}); err == nil {
		t.Fatal("expected error for a malformed CSV base, got nil")
	} else if !strings.Contains(err.Error(), "invalid ref") {
		t.Errorf("error should report the invalid ref, got: %v", err)
	}

	after, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Errorf("manifests-config.yaml was rewritten despite a malformed CSV base:\n%s", after)
	}
	goAfter, err := os.ReadFile(goFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(goAfter) != syncCodeDefaultsGoSource {
		t.Errorf("gateway_support.go was rewritten despite a malformed CSV base:\n%s", goAfter)
	}
}
