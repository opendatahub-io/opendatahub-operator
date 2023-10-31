// Package codeflare provides utility functions to config CodeFlare as part of the stack
// which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package codeflare

import (
	"fmt"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName       = "codeflare"
	CodeflarePath       = deploy.DefaultManifestPath + "/" + ComponentName + "/manifests"
	CodeflareOperator   = "codeflare-operator"
	RHCodeflareOperator = "rhods-codeflare-operator"
)

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

// Verifies that CodeFlare implements ComponentInterface.
var _ components.ComponentInterface = (*CodeFlare)(nil)

func (c *CodeFlare) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	var imageParamMap = map[string]string{
		"odh-codeflare-operator-controller-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE", // no need mcad, embedded in cfo
		"namespace": dscispec.ApplicationsNamespace,
	}
	enabled := c.GetManagementState() == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}
	if enabled {
		// Download manifests and update paths
		if err = c.OverrideManifests(string(platform)); err != nil {
			return err
		}
		// check if the CodeFlare operator is installed
		// codeflare operator not installed
		dependentOperator := CodeflareOperator
		// overwrite dependent operator if downstream not match upstream
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			dependentOperator = RHCodeflareOperator
		}

		if found, err := deploy.OperatorExists(cli, dependentOperator); err != nil {
			return fmt.Errorf("operator exists throws error %v", err)
		} else if found {
			return fmt.Errorf("operator %s  found. Please uninstall the operator before enabling %s component",
				dependentOperator, ComponentName)
		}

		// Update image parameters only when we do not have customized manifests set
		if dscispec.DevFlags.ManifestsUri == "" && len(c.DevFlags.Manifests) == 0 {
			if err := deploy.ApplyParams(CodeflarePath+"/base", c.SetImageParamsMap(imageParamMap), true); err != nil {
				return err
			}
		}
	}

	// Deploy Codeflare
	if err := deploy.DeployManifestsFromPath(cli, owner,
		CodeflarePath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return err
	}
	return nil
}

func (c *CodeFlare) DeepCopyInto(target *CodeFlare) {
	*target = *c
	target.Component = c.Component
}
