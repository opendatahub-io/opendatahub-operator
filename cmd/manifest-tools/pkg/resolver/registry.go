package resolver

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func ResolveDigestViaRegistry(ctx context.Context, imageRef string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	digest, err := crane.Digest(imageRef,
		crane.WithContext(ctx),
		crane.WithPlatform(&v1.Platform{
			Architecture: "amd64",
			OS:           "linux",
		}),
	)
	if err != nil {
		return "", fmt.Errorf("resolving digest for %s: %w", imageRef, err)
	}

	if digest == "" {
		return "", fmt.Errorf("empty digest for %s", imageRef)
	}

	return digest, nil
}

// ResolveImageLabelsViaRegistry returns the config labels of the linux/amd64
// image at imageRef. Konflux build pipelines stamp `release` with the source
// commit timestamp and `git.commit` with the source revision, which lets
// callers order builds from the same repository.
func ResolveImageLabelsViaRegistry(ctx context.Context, imageRef string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	img, err := crane.Pull(imageRef,
		crane.WithContext(ctx),
		crane.WithPlatform(&v1.Platform{
			Architecture: "amd64",
			OS:           "linux",
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("pulling config for %s: %w", imageRef, err)
	}

	cfgFile, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("reading config for %s: %w", imageRef, err)
	}
	if cfgFile == nil || cfgFile.Config.Labels == nil {
		return map[string]string{}, nil
	}
	return cfgFile.Config.Labels, nil
}
