package dscinitialization

import (
	"context"
	"fmt"
	"path/filepath"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

// CreateVAP creates VAP/VAPB for blocking Dashboard's HWProfile/AcceleratorProfile.
func (r *DSCInitializationReconciler) CreateVAP(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)

	// first check if CRDs in cluster, if no, quick exit
	apCRDExists, err := cluster.HasCRD(ctx, r.Client, gvk.DashboardAcceleratorProfile)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.DashboardAcceleratorProfile, err)
	}
	dhpCRDExists, err := cluster.HasCRD(ctx, r.Client, gvk.DashboardHardwareProfile)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.DashboardHardwareProfile, err)
	}

	// proceed if any CRDs exist
	if !apCRDExists && !dhpCRDExists {
		log.V(1).Info("Both CRD not exist, skipping handling for HardwareProfile")
		return nil
	}

	vapPath := filepath.Join(deploy.DefaultManifestPath, "hardwareprofiles", "vap")
	if err := deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, vapPath, "", "vap", true); err != nil {
		return fmt.Errorf("failed to deploy VAP/VAPB manifests from path %s: %w", vapPath, err)
	}

	log.V(1).Info("Successfully deployed VAP/VAPB resources")
	return nil
}

func (r *DSCInitializationReconciler) CreateHWProfileCR(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)

	// deploy hardware profile CR with dsci as owner, but allow user change by annotation set to false.
	hwProfilePath := filepath.Join(deploy.DefaultManifestPath, "hardwareprofiles")
	if err := deploy.DeployManifestsFromPath(ctx, r.Client, dscInit, hwProfilePath, dscInit.Spec.ApplicationsNamespace, "hardwareprofile", true); err != nil {
		return fmt.Errorf("failed to deploy HardwareProfile CR from path %s: %w", hwProfilePath, err)
	}

	log.V(1).Info("Successfully deployed HardwareProfile CR default-profile")
	return nil
}
