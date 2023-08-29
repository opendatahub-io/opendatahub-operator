// Package dashboard provides utility functions to config Open Data Hub Dashboard: A web dashboard that displays installed Open Data Hub components with easy access to component UIs and documentation
package dashboard

import (
	"fmt"

	"context"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName          = "odh-dashboard"
	ComponentNameSupported = "rhods-dashboard"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	PathSupported          = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/authentication"
	PathISVSM              = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/apps-onprem"
	PathISVAddOn           = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/apps-addon"
	PathOVMS               = deploy.DefaultManifestPath + "/" + ComponentName + "/modelserving"
	PathODHDashboardConfig = deploy.DefaultManifestPath + "/" + ComponentName + "/odhdashboardconfig"
	PathConsoleLink        = deploy.DefaultManifestPath + "/" + ComponentName + "/consolelink"
	NameConsoleLink        = "console"
	NamespaceConsoleLink   = "openshift-console"
	PathAnaconda           = deploy.DefaultManifestPath + "/partners/anaconda/base/"
)

var imageParamMap = map[string]string{
	"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
}

type Dashboard struct {
	components.Component `json:""`
}

func (d *Dashboard) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *Dashboard) GetComponentName() string {
	return ComponentName
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*Dashboard)(nil)

func (d *Dashboard) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, managementState operatorv1.ManagementState, namespace string) error {
	enabled := managementState == operatorv1.Managed

	// TODO: Add any additional tasks if required when reconciling component

	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		// Update Default rolebinding
		if platform == deploy.OpenDataHub || platform == "" {
			err := common.UpdatePodSecurityRolebinding(cli, []string{"odh-dashboard"}, namespace)
			if err != nil {
				return err
			}
		}
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			err := common.UpdatePodSecurityRolebinding(cli, []string{"rhods-dashboard"}, namespace)
			if err != nil {
				return err
			}
		}

		// Apply RHODS specific configs
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			// Replace admin group
			if platform == deploy.SelfManagedRhods {
				err = common.ReplaceStringsInFile(PathODHDashboardConfig+"/odhdashboardconfig.yaml", map[string]string{
					"<admin_groups>": "rhods-admins",
				})
				if err != nil {
					return err
				}
			} else if platform == deploy.ManagedRhods {
				err = common.ReplaceStringsInFile(PathODHDashboardConfig+"/odhdashboardconfig.yaml", map[string]string{
					"<admin_groups>": "dedicated-admins",
				})
				if err != nil {
					return err
				}
			}

			// Create ODHDashboardConfig if it doesn't exist already
			err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
				PathODHDashboardConfig,
				namespace,
				scheme, enabled)
			if err != nil {
				return fmt.Errorf("failed to set dashboard config from %s: %v", PathODHDashboardConfig, err)
			}

			// Apply modelserving config
			err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
				PathOVMS,
				namespace,
				scheme, enabled)
			if err != nil {
				return fmt.Errorf("failed to set dashboard OVMS from %s: %v", PathOVMS, err)
			}

			// Apply anaconda config
			err = common.CreateSecret(cli, "anaconda-ce-access", namespace)
			if err != nil {
				return fmt.Errorf("failed to create access-secret for anaconda: %v", err)
			}
			err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
				PathAnaconda,
				namespace,
				scheme, enabled)
			if err != nil {
				return fmt.Errorf("failed to deploy anaconda resources from %s: %v", PathAnaconda, err)
			}
		}
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
		return err
	}

	// Deploy odh-dashboard manifests
	if platform == deploy.OpenDataHub || platform == "" {
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
			Path,
			namespace,
			scheme, enabled)
		if err != nil {
			return err
		}
	} else if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
		// Apply authentication overlay
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathSupported,
			namespace,
			scheme, enabled)
		if err != nil {
			return err
		}
	}

	// ISV handling
	switch platform {
	case deploy.SelfManagedRhods:
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathISVSM,
			namespace,
			scheme, enabled)
		if err != nil {
			return fmt.Errorf("failed to set dashboard ISV from %s: %v", PathISVSM, err)
		}
		// ConsoleLink handling
		consoleRoute := &routev1.Route{}
		err = cli.Get(context.TODO(), client.ObjectKey{Name: NameConsoleLink, Namespace: NamespaceConsoleLink}, consoleRoute)
		if err != nil {
			return fmt.Errorf("error getting console route URL : %v", err)
		}
		domainIndex := strings.Index(consoleRoute.Spec.Host, ".")
		consolelinkDomain := consoleRoute.Spec.Host[domainIndex+1:]
		err = common.ReplaceStringsInFile(PathConsoleLink, map[string]string{
			"<rhods-dashboard-url>": "https://rhods-dashboard-" + namespace + "." + consolelinkDomain,
			"<section-title>": "OpenShift Self Managed Services",
		})
		if err != nil {
			return fmt.Errorf("error replacing with correct dashboard url for ConsoleLink: %v", err)
		}
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathConsoleLink,
			namespace,
			scheme, enabled)
		if err != nil {
			return fmt.Errorf("failed to set dashboard consolelink from %s", PathConsoleLink)
		}
		return err
	case deploy.ManagedRhods:
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathISVAddOn,
			namespace,
			scheme, enabled)
		if err != nil {
			return fmt.Errorf("failed to set dashboard ISV from %s: %v", PathISVAddOn, err)
		}
		// ConsoleLink handling
		consoleRoute := &routev1.Route{}
		err = cli.Get(context.TODO(), client.ObjectKey{Name: NameConsoleLink, Namespace: NamespaceConsoleLink}, consoleRoute)
		if err != nil {
			return fmt.Errorf("error getting console route URL : %v", err)
		}
		domainIndex := strings.Index(consoleRoute.Spec.Host, ".")
		consolelinkDomain := consoleRoute.Spec.Host[domainIndex+1:]
		err = common.ReplaceStringsInFile(PathConsoleLink, map[string]string{
			"<rhods-dashboard-url>": "https://rhods-dashboard-" + namespace + "." + consolelinkDomain,
			"<section-title>": "OpenShift Managed Services",
		})
		if err != nil {
			return fmt.Errorf("Error replacing with correct dashboard url for ConsoleLink: %v", err)
		}
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathConsoleLink,
			namespace,
			scheme, enabled)
		if err != nil {
			return fmt.Errorf("failed to set dashboard consolelink from %s", PathConsoleLink)
		}
		return err
	default:
		return nil
	}
}

func (in *Dashboard) DeepCopyInto(out *Dashboard) {
	*out = *in
	out.Component = in.Component
}
