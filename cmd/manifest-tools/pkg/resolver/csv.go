package resolver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

const (
	csvPath                    = "bundle/manifests/rhods-operator.clusterserviceversion.yaml"
	rhoaiProductionImagePrefix = "registry.redhat.io/rhoai/"
	rhoaiE2EImagePrefix        = "quay.io/rhoai/"
)

type CSVImage struct {
	Base   string
	Digest string
}

func FetchCSVRelatedImages(ctx context.Context, source config.BuildConfigRepo) (map[string]CSVImage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sha := config.ExtractSHA(source.Ref)
	if sha == "" {
		return nil, fmt.Errorf("Build-Config ref %q is not pinned to a commit SHA", source.Ref)
	}
	csvURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", source.Repo, sha, csvPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, csvURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching CSV: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CSV fetch returned %d", resp.StatusCode)
	}

	const maxCSVBodySize = 50 << 20 // 50 MiB
	limitedReader := io.LimitReader(resp.Body, maxCSVBodySize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("reading CSV body: %w", err)
	}
	if int64(len(body)) > maxCSVBodySize {
		return nil, fmt.Errorf("CSV body exceeds %d bytes", maxCSVBodySize)
	}

	return ParseCSVRelatedImages(body)
}

// NormalizeCSVImages converts production RHOAI registry references to their
// pullable e2e mirror. Other platforms and registry.redhat.io namespaces are
// left unchanged.
func NormalizeCSVImages(platform string, images map[string]CSVImage) map[string]CSVImage {
	if platform != "rhoai" {
		return images
	}

	normalized := make(map[string]CSVImage, len(images))
	for envName, image := range images {
		if strings.HasPrefix(image.Base, rhoaiProductionImagePrefix) {
			image.Base = rhoaiE2EImagePrefix + strings.TrimPrefix(image.Base, rhoaiProductionImagePrefix)
		}
		normalized[envName] = image
	}
	return normalized
}

func ParseCSVRelatedImages(data []byte) (map[string]CSVImage, error) {
	var csv struct {
		Spec struct {
			Install struct {
				Spec struct {
					Deployments []struct {
						Spec struct {
							Template struct {
								Spec struct {
									Containers []struct {
										Env []struct {
											Name  string `yaml:"name"`
											Value string `yaml:"value"`
										} `yaml:"env"`
									} `yaml:"containers"`
								} `yaml:"spec"`
							} `yaml:"template"`
						} `yaml:"spec"`
					} `yaml:"deployments"`
				} `yaml:"spec"`
			} `yaml:"install"`
		} `yaml:"spec"`
	}

	if err := yaml.Unmarshal(data, &csv); err != nil {
		return nil, fmt.Errorf("parsing CSV YAML: %w", err)
	}

	images := map[string]CSVImage{}
	for _, dep := range csv.Spec.Install.Spec.Deployments {
		for _, container := range dep.Spec.Template.Spec.Containers {
			for _, env := range container.Env {
				if !strings.HasPrefix(env.Name, "RELATED_IMAGE_") {
					continue
				}
				base, digest, ok := strings.Cut(env.Value, "@")
				if !ok || !strings.HasPrefix(digest, "sha256:") {
					slog.Debug("CSV entry skipped (no digest)", slog.String("env", env.Name))
					continue
				}
				images[env.Name] = CSVImage{Base: base, Digest: digest}
			}
		}
	}

	return images, nil
}
