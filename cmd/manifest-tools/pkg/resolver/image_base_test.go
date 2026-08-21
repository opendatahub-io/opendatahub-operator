package resolver

import "testing"

func TestImageBaseWithoutTag(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{ref: "quay.io/opendatahub/foo:tag", want: "quay.io/opendatahub/foo"},
		{ref: "registry.example:5000/ns/img:tag", want: "registry.example:5000/ns/img"},
		{ref: "localhost:5000/img:tag", want: "localhost:5000/img"},
		{ref: "nginx:latest", want: "nginx"},
		{ref: "quay.io/opendatahub/foo", want: "quay.io/opendatahub/foo"},
		{ref: "registry.example:5000/ns/img", want: "registry.example:5000/ns/img"},
		{ref: "quay.io/opendatahub/foo@sha256:abc", want: "quay.io/opendatahub/foo"},
	}
	for _, tt := range tests {
		got := imageBaseWithoutTag(tt.ref)
		if got != tt.want {
			t.Errorf("imageBaseWithoutTag(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}
