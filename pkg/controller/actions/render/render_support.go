package render

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type CachingKeyFn func(_ context.Context, rr *types.ReconciliationRequest) ([]byte, error)

func DefaultCachingKeyFn(_ context.Context, rr *types.ReconciliationRequest) ([]byte, error) {
	hash := sha256.New()

	dsciGeneration := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(dsciGeneration, rr.DSCI.GetGeneration())

	instanceGeneration := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(instanceGeneration, rr.Instance.GetGeneration())

	if _, err := hash.Write(dsciGeneration); err != nil {
		return nil, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
	}
	if _, err := hash.Write(instanceGeneration); err != nil {
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
	for i := range rr.Templates {
		if _, err := hash.Write([]byte(rr.Templates[i].Path)); err != nil {
			return nil, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
		}
	}

	return hash.Sum(nil), nil
}
