package kustomize

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func DefaultCachingKeyFn(_ context.Context, rr *types.ReconciliationRequest) ([]byte, error) {
	hash := sha256.New()

	generation := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(generation, rr.Instance.GetGeneration())

	if _, err := hash.Write(generation); err != nil {
		return nil, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
	}
	if _, err := hash.Write([]byte(rr.Release.Name)); err != nil {
		return nil, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
	}
	if _, err := hash.Write([]byte(rr.Release.Version.String())); err != nil {
		return nil, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
	}

	for i := range rr.Manifests {
		if _, err := hash.Write([]byte(rr.Manifests[i].String())); err != nil {
			return nil, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
		}
	}

	return hash.Sum(nil), nil
}
