package dscinitialization

import (
	"context"
	"fmt"
	"path/filepath"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

// deploy default hardware profile CR with dsci as owner, but allow user change by annotation set to false.
func (r *DSCInitializationReconciler) ManageDefaultAndCustomHWProfileCR(ctx context.Context, dscInit *dsciv2.DSCInitialization, platform common.Platform) error {
	log := logf.FromContext(ctx)

	if platform == "" { // this is for test to skip creation.
		log.V(1).Info("Skipping HardwareProfile CR creation if platform is not set")
		return nil
	}

	// Check if default HardwareProfile CR already exists.
	_, defaultProfileError := cluster.GetHardwareProfile(ctx, r.Client, "default-profile", dscInit.Spec.ApplicationsNamespace)

	if client.IgnoreNotFound(defaultProfileError) != nil {
		return fmt.Errorf("failed to check HardwareProfile CR: default-profile %w", defaultProfileError)
	}

	if k8serr.IsNotFound(defaultProfileError) {
		// deploy default hardware profile CR with dsci as owner, but allow user change by have annotation in the default.
		defaultProfilePath := filepath.Join(deploy.DefaultManifestPath, "hardwareprofiles")
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, defaultProfilePath, dscInit.Spec.ApplicationsNamespace, "hardwareprofile", true); err != nil {
			return fmt.Errorf("failed to deploy default-profile HardwareProfile CR from path %s: %w", defaultProfilePath, err)
		}
		log.V(1).Info("Successfully deployed default-profile HardwareProfile CR")
	}
	return nil
}
