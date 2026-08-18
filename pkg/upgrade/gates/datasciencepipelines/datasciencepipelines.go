package datasciencepipelines

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	dspaCRDName             = "datasciencepipelinesapplications.datasciencepipelinesapplications.opendatahub.io"
	deprecatedStoredVersion = "v1alpha1"
)

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := reader.Get(ctx, client.ObjectKey{Name: dspaCRDName}, crd)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("getting DataSciencePipelinesApplication CRD: %w", err)
	}

	if !hasStoredVersion(crd.Status.StoredVersions, deprecatedStoredVersion) {
		return nil
	}

	return &UpgradeBlockedError{StoredVersion: deprecatedStoredVersion}
}

func hasStoredVersion(storedVersions []string, version string) bool {
	for _, storedVersion := range storedVersions {
		if storedVersion == version {
			return true
		}
	}

	return false
}
