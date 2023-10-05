// Package codeflare provides utility functions to config CodeFlare as part of the stack
// which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package codeflare

import (
	"fmt"
	"path/filepath"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		defaultKustomizePath := "base"
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
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	dependentOperator := CodeflareOperator
	// overwrite dependent operator if downstream not match upstream
	if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
		dependentOperator = RHCodeflareOperator
	}
	// Download manifests and update paths
	if err = c.OverrideManifests(string(platform)); err != nil {
		return err
	}
	// Update image parameters only when we do not have customized manifests set
	if dscispec.DevFlags.ManifestsUri == "" && len(c.DevFlags.Manifests) == 0 {
		if err := deploy.ApplyParams(CodeflarePath+"/base", c.SetImageParamsMap(imageParamMap), true); err != nil {
			return err
		}
	}
	if c.GetManagementState() == operatorv1.Managed {
		// check if the CodeFlare operator is installed
		if found, err := deploy.OperatorExists(cli, dependentOperator); err != nil {
			return err
		} else if found {
			return fmt.Errorf("operator %s already installed in cluster. Please uninstall the operator before enabling %s component",
				dependentOperator, ComponentName)
		}
		// Deploy Codeflare
		err = deploy.DeployManifestsFromPath(cli, owner, CodeflarePath, dscispec.ApplicationsNamespace, c.GetComponentName(), operatorv1.Managed)
		return err
	}
	return nil
}

func (c *CodeFlare) DeepCopyInto(target *CodeFlare) {
	*target = *c
	target.Component = c.Component
}
