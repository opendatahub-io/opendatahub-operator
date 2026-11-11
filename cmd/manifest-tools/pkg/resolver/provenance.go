package resolver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

type RegistryImageResolver func(context.Context, string) (RegistryImage, error)

type SourceRevisionFetcher func(context.Context, config.PlatformRepo, string) (string, bool, error)

type sourceMetadata struct {
	Revision string `yaml:"revision"`
}

type ProvenanceOptions struct {
	ConfigFile           string
	ResolveRegistryImage RegistryImageResolver
	FetchSourceRevision  SourceRevisionFetcher
}

func ValidateProvenance(ctx context.Context, opts ProvenanceOptions) error {
	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return err
	}
	resolve := opts.ResolveRegistryImage
	if resolve == nil {
		resolve = ResolveRegistryImage
	}
	fetch := opts.FetchSourceRevision
	if fetch == nil {
		fetch = FetchSourceRevision
	}

	for envName, override := range cfg.ImageOverrides {
		if err := cfg.CheckImageOverride(envName, override); err != nil {
			return err
		}
		component := cfg.FindComponent(override.Component)
		for _, platform := range []string{"odh", "rhoai"} {
			image := override.PlatformImage(platform)
			if image == nil || (image.ExpectedRevision == "" && image.SourceRevisionPath == "") {
				continue
			}
			if !config.DigestPattern.MatchString(image.Digest) || image.Base == "" {
				return fmt.Errorf("imageOverrides.%s.%s: base and digest are required for provenance validation", envName, platform)
			}
			var repo *config.PlatformRepo
			if component != nil {
				repo = component.PlatformRepo(platform)
			}
			if repo == nil {
				return fmt.Errorf("imageOverrides.%s.%s: component has no platform repository", envName, platform)
			}
			if err := validateSelectedImage(ctx, envName, platform, image.Base, image.Digest, image, repo, false, nil, resolve, fetch); err != nil {
				return err
			}
		}
	}
	return nil
}

func FetchSourceRevision(ctx context.Context, repo config.PlatformRepo, sourcePath string) (string, bool, error) {
	sha := config.ExtractSHA(repo.Ref)
	if sha == "" {
		return "", false, fmt.Errorf("repository ref %q has no commit SHA", repo.Ref)
	}

	endpoint := url.URL{
		Scheme: "https",
		Host:   "raw.githubusercontent.com",
		Path:   path.Join(repo.Repo, sha, sourcePath),
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", false, fmt.Errorf("creating source metadata request: %w", err)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", false, fmt.Errorf("fetching %s: %w", endpoint.String(), err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if response.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("fetching %s: HTTP %s", endpoint.String(), response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", endpoint.String(), err)
	}

	var metadata sourceMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return "", false, fmt.Errorf("parsing %s: %w", endpoint.String(), err)
	}
	if !fullCommitSHA(metadata.Revision) {
		return "", false, fmt.Errorf("%s: revision %q must be a 40-character commit SHA", endpoint.String(), metadata.Revision)
	}
	return metadata.Revision, true, nil
}

func expectedImageRevision(
	ctx context.Context,
	image *config.PlatformImage,
	repo *config.PlatformRepo,
	automaticComponent bool,
	fetch SourceRevisionFetcher,
) (string, bool, error) {
	if image == nil || repo == nil {
		return "", false, nil
	}
	if automaticComponent || image.ExpectedRevision == config.ExpectedRevisionComponent {
		sha := config.ExtractSHA(repo.Ref)
		if !fullCommitSHA(sha) {
			return "", false, fmt.Errorf("component ref %q must contain a 40-character commit SHA", repo.Ref)
		}
		return sha, true, nil
	}
	if image.SourceRevisionPath == "" {
		return "", false, nil
	}
	return fetch(ctx, *repo, image.SourceRevisionPath)
}

func validateSelectedImage(
	ctx context.Context,
	envName, platform, base, digest string,
	image *config.PlatformImage,
	repo *config.PlatformRepo,
	automaticComponent bool,
	resolved *RegistryImage,
	resolve RegistryImageResolver,
	fetch SourceRevisionFetcher,
) error {
	expected, validate, err := expectedImageRevision(ctx, image, repo, automaticComponent, fetch)
	if err != nil {
		return fmt.Errorf("%s %s provenance: %w", envName, platform, err)
	}
	if !validate {
		return nil
	}

	metadata := RegistryImage{}
	if resolved != nil {
		metadata = *resolved
	} else {
		metadata, err = resolve(ctx, base+"@"+digest)
		if err != nil {
			return fmt.Errorf("%s %s provenance: %w", envName, platform, err)
		}
	}
	if metadata.Revision == "" {
		return nil
	}
	if metadata.Revision != expected {
		return fmt.Errorf("%s %s image revision %s does not match expected revision %s", envName, platform, metadata.Revision, expected)
	}
	return nil
}

func fullCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}
