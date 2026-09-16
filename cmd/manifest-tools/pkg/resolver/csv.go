package resolver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultCSVURL = "https://raw.githubusercontent.com/red-hat-data-services/RHOAI-Build-Config/rhoai-2.25/bundle/manifests/rhods-operator.clusterserviceversion.yaml"

type CSVImage struct {
	Base   string
	Digest string
}

func FetchCSVRelatedImages(ctx context.Context) (map[string]CSVImage, error) {
	url := os.Getenv("CSV_URL")
	if url == "" {
		url = defaultCSVURL
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
				// Map registry.redhat.io/rhoai to quay.io/rhoai for CI pull secret compatibility
				base = strings.Replace(base, "registry.redhat.io/rhoai", "quay.io/rhoai", 1)
				images[env.Name] = CSVImage{Base: base, Digest: digest}
			}
		}
	}

	return images, nil
}
