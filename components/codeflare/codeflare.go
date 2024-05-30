// Package codeflare provides utility functions to config CodeFlare as part of the stack
// which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
// +groupName=datasciencecluster.opendatahub.io
package codeflare

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName     = "codeflare"
	CodeflarePath     = deploy.DefaultManifestPath + "/" + ComponentName + "/default"
	CodeflareOperator = "codeflare-operator"
	ParamsPath        = deploy.DefaultManifestPath + "/" + ComponentName + "/manager"
)

// Verifies that CodeFlare implements ComponentInterface.
var _ components.ComponentInterface = (*CodeFlare)(nil)

// CodeFlare struct holds the configuration for the CodeFlare component.
// +kubebuilder:object:generate=true
type CodeFlare struct {
	components.Component `json:""`
}

func (c *CodeFlare) OverrideManifests(_ string) error {
	// If devflags are set, update default manifests path
	if len(c.DevFlags.Manifests) != 0 {
		manifestConfig := c.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "default"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		CodeflarePath = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}

	return nil
}

func (c *CodeFlare) GetComponentName() string {
	return ComponentName
}

func (c *CodeFlare) ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	l := c.ConfigComponentLogger(logger, ComponentName, dscispec)
	var imageParamMap = map[string]string{
		"codeflare-operator-controller-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE", // no need mcad, embedded in cfo
		"namespace":                           dscispec.ApplicationsNamespace,
	}

	enabled := c.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := cluster.GetPlatform(cli)
	if err != nil {
		return err
	}
	if enabled {
		if c.DevFlags != nil {
			// Download manifests and update paths
			if err = c.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		// check if the CodeFlare operator is installed: it should not be installed
		// Both ODH and RHOAI should have the same operator name
		dependentOperator := CodeflareOperator

		if found, err := cluster.OperatorExists(cli, dependentOperator); err != nil {
			return fmt.Errorf("operator exists throws error %w", err)
		} else if found {
			return fmt.Errorf("operator %s is found. Please uninstall the operator before enabling %s component",
				dependentOperator, ComponentName)
		}

		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (c.DevFlags == nil || len(c.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(ParamsPath, imageParamMap, true); err != nil {
				return fmt.Errorf("failed update image from %s : %w", CodeflarePath+"/bases", err)
			}
		}
	}

	// Deploy Codeflare
	if err := deploy.DeployManifestsFromPath(cli, owner, //nolint:revive,nolintlint
		CodeflarePath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return err
	}
	l.Info("apply manifests done")
	// CloudServiceMonitoring handling
	if platform == cluster.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus won't fire alerts when it is just startup
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			l.Info("deployment is done, updating monitoring rules")
		}

		// inject prometheus codeflare*.rules in to /opt/manifests/monitoring/prometheus/prometheus-configs.yaml
		if err = c.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
		l.Info("updating SRE monitoring done")
	}

	return nil
}
