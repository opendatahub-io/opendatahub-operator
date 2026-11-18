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

// maxConfigBytes caps the image config blob this resolver is willing to
// buffer. Real config files are a few kilobytes; the cap only rejects
// absurd sizes declared by a broken or hostile registry.
const maxConfigBytes = 4 * 1024 * 1024

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

	// Cap the config blob size declared in the manifest before
	// ConfigFile buffers it, so a hostile or broken registry cannot make
	// the resolver allocate unbounded memory (CWE-789).
	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("reading manifest for %s: %w", imageRef, err)
	}
	if manifest.Config.Size > maxConfigBytes {
		return nil, fmt.Errorf("config for %s exceeds size limit: %d bytes", imageRef, manifest.Config.Size)
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
