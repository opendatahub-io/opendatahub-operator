package dscinitialization

import (
	"context"
	"fmt"
	"path/filepath"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func (r *DSCInitializationReconciler) CreateHWProfileCR(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)

	// deploy hardware profile CR with dsci as owner, but allow user change by have annotation in the default.
	hwProfilePath := filepath.Join(deploy.DefaultManifestPath, "hardwareprofiles")
	if err := deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, hwProfilePath, dscInit.Spec.ApplicationsNamespace, "hardwareprofile", true); err != nil {
		return fmt.Errorf("failed to deploy HardwareProfile CR from path %s: %w", hwProfilePath, err)
	}

	log.V(1).Info("Successfully deployed HardwareProfile CR default-profile")
	return nil
}
