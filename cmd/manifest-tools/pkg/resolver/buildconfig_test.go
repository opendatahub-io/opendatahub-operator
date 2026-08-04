package resolver_test

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func TestSplitImageRef(t *testing.T) {
	tests := []struct {
		ref        string
		wantBase   string
		wantDigest string
	}{
		{
			"quay.io/opendatahub/dashboard@sha256:abc123",
			"quay.io/opendatahub/dashboard",
			"sha256:abc123",
		},
		{
			"quay.io/opendatahub/dashboard:latest",
			"quay.io/opendatahub/dashboard:latest",
			"",
		},
		{
			"registry.redhat.io/rhoai/image@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc",
			"registry.redhat.io/rhoai/image",
			"sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc",
		},
	}

	for _, tt := range tests {
		base, digest := resolver.SplitImageRef(tt.ref)
		if base != tt.wantBase {
			t.Errorf("SplitImageRef(%q) base = %q, want %q", tt.ref, base, tt.wantBase)
		}
		if digest != tt.wantDigest {
			t.Errorf("SplitImageRef(%q) digest = %q, want %q", tt.ref, digest, tt.wantDigest)
		}
	}
}

func TestFetchBuildConfigImages_InvalidRepo(t *testing.T) {
	_, err := resolver.FetchBuildConfigImages("evil-org/evil-repo", "main")
	if err == nil {
		t.Error("expected error for non-allowlisted repo")
	}
}
