// Package codeflare provides utility functions to config CodeFlare as part of the stack
// which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package codeflare

import (
	"context"
	"fmt"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/monitoring"
)

var (
	ComponentName       = "codeflare"
	CodeflarePath       = deploy.DefaultManifestPath + "/" + ComponentName + "/manifests"
	CodeflareOperator   = "codeflare-operator"
	RHCodeflareOperator = "rhods-codeflare-operator"
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
		defaultKustomizePath := "manifests"
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

func (c *CodeFlare) ReconcileComponent(ctx context.Context, cli client.Client, resConf *rest.Config, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	var imageParamMap = map[string]string{
		"odh-codeflare-operator-controller-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE", // no need mcad, embedded in cfo
		"namespace": dscispec.ApplicationsNamespace,
	}
	enabled := c.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
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
		dependentOperator := CodeflareOperator
		// overwrite dependent operator if downstream not match upstream
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			dependentOperator = RHCodeflareOperator
		}

		if found, err := deploy.OperatorExists(cli, dependentOperator); err != nil {
			return fmt.Errorf("operator exists throws error %w", err)
		} else if found {
			return fmt.Errorf("operator %s is found. Please uninstall the operator before enabling %s component",
				dependentOperator, ComponentName)
		}

		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (c.DevFlags == nil || len(c.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(CodeflarePath+"/bases", c.SetImageParamsMap(imageParamMap), true); err != nil {
				return err
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

	// CloudServiceMonitoring handling
	if platform == deploy.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus wont fire alerts when it is just startup
			if err := monitoring.WaitForDeploymentAvailable(ctx, resConf, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			fmt.Printf("deployment for %s is done, updating monitoring rules\n", ComponentName)
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
	}

	return nil
}
