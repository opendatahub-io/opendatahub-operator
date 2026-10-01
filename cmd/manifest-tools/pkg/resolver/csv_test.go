package resolver_test

import (
	"context"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func TestParseCSVRelatedImages(t *testing.T) {
	csvYAML := `
spec:
  install:
    spec:
      deployments:
      - spec:
          template:
            spec:
              containers:
              - env:
                - name: RELATED_IMAGE_ODH_DASHBOARD_IMAGE
                  value: "quay.io/opendatahub/odh-dashboard@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
                - name: RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE
                  value: "quay.io/opendatahub/odh-model-controller@sha256:1111111111111111111111111111111111111111111111111111111111111111"
                - name: NOT_A_RELATED_IMAGE
                  value: "should-be-skipped"
                - name: RELATED_IMAGE_TAGGED_ONLY
                  value: "quay.io/opendatahub/some-image:latest"
                - name: OPERATOR_CONDITION_NAME
                  value: "some-condition"
`

	images, err := resolver.ParseCSVRelatedImages([]byte(csvYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}

	dashboard, ok := images["RELATED_IMAGE_ODH_DASHBOARD_IMAGE"]
	if !ok {
		t.Fatal("RELATED_IMAGE_ODH_DASHBOARD_IMAGE not found")
	}
	if dashboard.Base != "quay.io/opendatahub/odh-dashboard" {
		t.Errorf("base = %q, want quay.io/opendatahub/odh-dashboard", dashboard.Base)
	}
	if dashboard.Digest != "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" {
		t.Errorf("digest = %q", dashboard.Digest)
	}

	model, ok := images["RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE"]
	if !ok {
		t.Fatal("RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE not found")
	}
	if model.Base != "quay.io/opendatahub/odh-model-controller" {
		t.Errorf("base = %q", model.Base)
	}

	if _, ok := images["NOT_A_RELATED_IMAGE"]; ok {
		t.Error("non-RELATED_IMAGE entry should be skipped")
	}
	if _, ok := images["RELATED_IMAGE_TAGGED_ONLY"]; ok {
		t.Error("tagged-only entry (no @sha256:) should be skipped")
	}
}

func TestParseCSVRelatedImages_MultipleDeployments(t *testing.T) {
	csvYAML := `
spec:
  install:
    spec:
      deployments:
      - spec:
          template:
            spec:
              containers:
              - env:
                - name: RELATED_IMAGE_ODH_FIRST
                  value: "quay.io/first@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      - spec:
          template:
            spec:
              containers:
              - env:
                - name: RELATED_IMAGE_ODH_SECOND
                  value: "quay.io/second@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`

	images, err := resolver.ParseCSVRelatedImages([]byte(csvYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 2 {
		t.Fatalf("expected 2 images from 2 deployments, got %d", len(images))
	}
	if _, ok := images["RELATED_IMAGE_ODH_FIRST"]; !ok {
		t.Error("missing RELATED_IMAGE_ODH_FIRST")
	}
	if _, ok := images["RELATED_IMAGE_ODH_SECOND"]; !ok {
		t.Error("missing RELATED_IMAGE_ODH_SECOND")
	}
}

func TestParseCSVRelatedImages_EmptySpec(t *testing.T) {
	csvYAML := `
spec:
  install:
    spec:
      deployments: []
`

	images, err := resolver.ParseCSVRelatedImages([]byte(csvYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
}

func TestParseCSVRelatedImages_InvalidYAML(t *testing.T) {
	_, err := resolver.ParseCSVRelatedImages([]byte(`{invalid yaml`))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestNormalizeCSVImages_RHOAIRegistryOnly(t *testing.T) {
	images := map[string]resolver.CSVImage{
		"RELATED_IMAGE_RHOAI": {Base: "registry.redhat.io/rhoai/example-rhel9", Digest: "sha256:aaa"},
		"RELATED_IMAGE_UBI":   {Base: "registry.redhat.io/ubi9/ubi-minimal", Digest: "sha256:bbb"},
		"RELATED_IMAGE_ODH":   {Base: "quay.io/opendatahub/example", Digest: "sha256:ccc"},
	}

	rhoai := resolver.NormalizeCSVImages("rhoai", images)
	if got := rhoai["RELATED_IMAGE_RHOAI"].Base; got != "quay.io/rhoai/example-rhel9" {
		t.Errorf("normalized RHOAI base = %q", got)
	}
	if got := rhoai["RELATED_IMAGE_UBI"].Base; got != "registry.redhat.io/ubi9/ubi-minimal" {
		t.Errorf("non-RHOAI Red Hat image was rewritten: %q", got)
	}
	if got := images["RELATED_IMAGE_RHOAI"].Base; got != "registry.redhat.io/rhoai/example-rhel9" {
		t.Errorf("input map was mutated: %q", got)
	}
	if got := resolver.NormalizeCSVImages("odh", images)["RELATED_IMAGE_RHOAI"].Base; got != "registry.redhat.io/rhoai/example-rhel9" {
		t.Errorf("ODH image was rewritten: %q", got)
	}
}

func TestFetchCSVRelatedImages_RequiresPinnedRef(t *testing.T) {
	_, err := resolver.FetchCSVRelatedImages(context.Background(), config.BuildConfigRepo{
		Repo: "opendatahub-io/ODH-Build-Config",
		Ref:  "main",
	})
	if err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("FetchCSVRelatedImages() error = %v, want pinned-ref error", err)
	}
}
