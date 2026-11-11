package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

type RegistryImage struct {
	Digest   string
	Revision string
}

func ResolveRegistryImage(ctx context.Context, imageRef string) (RegistryImage, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	options := []crane.Option{
		crane.WithContext(ctx),
		crane.WithPlatform(&v1.Platform{
			Architecture: "amd64",
			OS:           "linux",
		}),
	}
	digest, err := crane.Digest(imageRef, options...)
	if err != nil {
		return RegistryImage{}, fmt.Errorf("resolving digest for %s: %w", imageRef, err)
	}
	if digest == "" {
		return RegistryImage{}, fmt.Errorf("empty digest for %s", imageRef)
	}

	configJSON, err := crane.Config(imageRef, options...)
	if err != nil {
		return RegistryImage{}, fmt.Errorf("reading image config for %s: %w", imageRef, err)
	}
	var configFile v1.ConfigFile
	if err := json.Unmarshal(configJSON, &configFile); err != nil {
		return RegistryImage{}, fmt.Errorf("parsing image config for %s: %w", imageRef, err)
	}

	return RegistryImage{
		Digest:   digest,
		Revision: configFile.Config.Labels["org.opencontainers.image.revision"],
	}, nil
}

func ResolveDigestViaRegistry(ctx context.Context, imageRef string) (string, error) {
	image, err := ResolveRegistryImage(ctx, imageRef)
	if err != nil {
		return "", err
	}
	return image.Digest, nil
}
