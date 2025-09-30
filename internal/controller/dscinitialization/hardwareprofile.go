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

// deploy hardware profile CR with dsci as owner, but allow user change by annotation set to false.
func (r *DSCInitializationReconciler) ManageDefaultAndCustomHWProfileCR(ctx context.Context, dscInit *dsciv2.DSCInitialization, platform common.Platform) error {
	log := logf.FromContext(ctx)

	if platform == "" { // this is for test to skip creation.
		log.V(1).Info("Skipping HardwareProfile CR creation if platform is not set")
		return nil
	}

	// Check if default HardwareProfile CR already exists.
	_, err := cluster.GetHardwareProfile(ctx, r.Client, "default-profile", dscInit.Spec.ApplicationsNamespace)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to check default HardwareProfile CR: %w", err)
	}

	if k8serr.IsNotFound(err) {
		// deploy hardware profile CR with dsci as owner, but allow user change by have annotation in the default.
		hwProfilePath := filepath.Join(deploy.DefaultManifestPath, "hardwareprofiles")
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, hwProfilePath, dscInit.Spec.ApplicationsNamespace, "hardwareprofile", true); err != nil {
			return fmt.Errorf("failed to deploy HardwareProfile CR from path %s: %w", hwProfilePath, err)
		}
	}
	// Check if custom-serving HardwareProfile CR already exists
	_, err = cluster.GetHardwareProfile(ctx, r.Client, "custom-serving", dscInit.Spec.ApplicationsNamespace)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to check custom-serving HardwareProfile CR: %w", err)
	}

	if k8serr.IsNotFound(err) {
		hwProfilePath := filepath.Join(deploy.DefaultManifestPath, "hardwareprofiles")
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, hwProfilePath, dscInit.Spec.ApplicationsNamespace, "hardwareprofile", true); err != nil {
			return fmt.Errorf("failed to deploy HardwareProfile CR from path %s: %w", hwProfilePath, err)
		}
	}

	log.V(1).Info("Successfully deployed default and custom-serving HardwareProfile CR")
	return nil
}
