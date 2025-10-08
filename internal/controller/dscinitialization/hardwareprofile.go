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
	_, defaultProfileError := cluster.GetHardwareProfile(ctx, r.Client, "default-profile", dscInit.Spec.ApplicationsNamespace)
	// Check if custom-serving HardwareProfile CR already exists
	_, customServingError := cluster.GetHardwareProfile(ctx, r.Client, "custom-serving", dscInit.Spec.ApplicationsNamespace)

	if client.IgnoreNotFound(defaultProfileError) != nil || client.IgnoreNotFound(customServingError) != nil {
		return fmt.Errorf("failed to check HardwareProfile CR: default-profile %w, custom-serving %w", defaultProfileError, customServingError)
	}

	if k8serr.IsNotFound(defaultProfileError) || k8serr.IsNotFound(customServingError) {
		// deploy hardware profile CRs with dsci as owner, but allow user change by have annotation in the default.
		// default and custom hardwareprofile CRs are stored in the config/hardwareprofiles directory.
		// so by deploying the path, Kustomize will deploy either or both depending on the CRs present.
		hwProfilePath := filepath.Join(deploy.DefaultManifestPath, "hardwareprofiles")
		if err := deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, hwProfilePath, dscInit.Spec.ApplicationsNamespace, "hardwareprofile", true); err != nil {
			return fmt.Errorf("failed to deploy HardwareProfile CR from path %s: %w", hwProfilePath, err)
		}
		log.V(1).Info("Successfully deployed HardwareProfile CRs")
	}
	return nil
}
