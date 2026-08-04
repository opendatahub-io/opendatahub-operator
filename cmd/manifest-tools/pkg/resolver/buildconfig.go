package resolver

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var allowedRepos = map[string]bool{
	"opendatahub-io/ODH-Build-Config":            true,
	"red-hat-data-services/RHOAI-Build-Config":    true,
}

var digestRefPattern = regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)

type csvSpec struct {
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

func FetchBuildConfigImages(repo, branch string) (map[string]string, error) {
	if !allowedRepos[repo] {
		return nil, fmt.Errorf("repository %q not in allowlist", repo)
	}

	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/bundle/manifests/rhods-operator.clusterserviceversion.yaml", repo, branch)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching Build-Config CSV: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Build-Config CSV returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading Build-Config CSV: %w", err)
	}

	var csv csvSpec
	if err := yaml.Unmarshal(body, &csv); err != nil {
		return nil, fmt.Errorf("parsing Build-Config CSV: %w", err)
	}

	images := make(map[string]string)
	for _, deploy := range csv.Spec.Install.Spec.Deployments {
		for _, container := range deploy.Spec.Template.Spec.Containers {
			for _, env := range container.Env {
				if !strings.HasPrefix(env.Name, "RELATED_IMAGE_") {
					continue
				}
				if !digestRefPattern.MatchString(env.Value) {
					slog.Warn("Skipping env var, value does not contain valid @sha256 digest", slog.String("name", env.Name))
					continue
				}
				images[env.Name] = env.Value
			}
		}
	}

	return images, nil
}

func SplitImageRef(ref string) (base, digest string) {
	idx := strings.LastIndex(ref, "@")
	if idx < 0 {
		return ref, ""
	}
	return ref[:idx], ref[idx+1:]
}
