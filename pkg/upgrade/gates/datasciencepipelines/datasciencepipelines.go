package datasciencepipelines

import (
	"context"
	"fmt"
	"slices"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	dspaCRDName             = "datasciencepipelinesapplications.datasciencepipelinesapplications.opendatahub.io"
	deprecatedStoredVersion = "v1alpha1"
)

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	blocking := &UpgradeBlockedError{}

	storedVersion, err := validateStoredVersion(ctx, reader)
	if err != nil {
		return err
	}

	blocking.StoredVersion = storedVersion

	if blocking.StoredVersion == "" {
		return nil
	}

	return blocking
}

func validateStoredVersion(ctx context.Context, reader client.Reader) (string, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := reader.Get(ctx, client.ObjectKey{Name: dspaCRDName}, crd)
	switch {
	case k8serr.IsNotFound(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("getting DataSciencePipelinesApplication CRD: %w", err)
	case slices.Contains(crd.Status.StoredVersions, deprecatedStoredVersion):
		return deprecatedStoredVersion, nil
	default:
		return "", nil
	}
}
