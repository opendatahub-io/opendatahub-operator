package resolver_test

import (
	"os"
	"path/filepath"
	"testing"

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

func TestFetchBuildConfigImages_AllowedRepos(t *testing.T) {
	allowedRepos := []string{
		"opendatahub-io/ODH-Build-Config",
		"red-hat-data-services/RHOAI-Build-Config",
	}

	for _, repo := range allowedRepos {
		_, err := resolver.FetchBuildConfigImages(repo, "nonexistent-branch-for-test")
		if err == nil {
			continue
		}
		// Should NOT be an allowlist error — should be a fetch error
		if err.Error() == "repository \""+repo+"\" not in allowlist" {
			t.Errorf("repo %q should be in allowlist but got rejection", repo)
		}
	}

	rejectedRepos := []string{
		"evil-org/evil-repo",
		"opendatahub-io/not-build-config",
		"",
	}

	for _, repo := range rejectedRepos {
		_, err := resolver.FetchBuildConfigImages(repo, "main")
		if err == nil {
			t.Errorf("repo %q should be rejected but was accepted", repo)
		}
	}
}
