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
