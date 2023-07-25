package dashboard

import (
	"fmt"

	"context"
	"strings"

	"github.com/go-logr/logr"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
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

func (d *Dashboard) ReconcileComponent(
	owner metav1.Object,
	cli client.Client,
	scheme *runtime.Scheme,
	enabled bool,
	namespace string,
	logger logr.Logger,
) error {

	// TODO: Add any additional tasks if required when reconciling component

	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		logger.Error(err, "Failed to determinate which platform running on")
		return err
	}

	// Update Default rolebinding
	var sa []string
	if platform == deploy.OpenDataHub {
		sa = []string{"odh-dashboard"}
		err = common.UpdatePodSecurityRolebinding(cli, sa, namespace)
	} else {
		sa = []string{"rhods-dashboard"}
		err = common.UpdatePodSecurityRolebinding(cli, sa, namespace)
	}
	if err != nil {
		logger.Error(err, "Failed to update rolebinding for serviceaccount "+fmt.Sprint(sa)+" in "+namespace)
		return err
	}

	// Apply RHODS specific configs
	if platform != deploy.OpenDataHub {
		// Replace admin groups
		if platform == deploy.SelfManagedRhods {
			err = common.ReplaceStringsInFile(PathODHDashboardConfig+"/odhdashboardconfig.yaml", map[string]string{
				"<admin_groups>": "rhods-admins",
			})
		} else {
			err = common.ReplaceStringsInFile(PathODHDashboardConfig+"/odhdashboardconfig.yaml", map[string]string{
				"<admin_groups>": "dedicated-admins",
			})
		}
		if err != nil {
			logger.Error(err, "Failed to replace admin_groups in manifests for "+string(platform))
			return err
		}

		// Create ODHDashboardConfig if it doesn't exist already
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathODHDashboardConfig,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			logger.Error(err, "Failed to set dashboard config", "location", PathODHDashboardConfig)
			return err
		}

		// Apply OVMS config
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathOVMS,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			logger.Error(err, "Failed to set dashboard OVMS", "location", PathOVMS)
			return err
		}

		// Apply anaconda config
		err = common.CreateSecret(cli, "anaconda-ce-access", namespace)
		if err != nil {
			return fmt.Errorf("Failed to create access-secret for anaconda: %v", err)
		}
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathAnaconda,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			return fmt.Errorf("Failed to deploy anaconda resources from %s: %v", PathAnaconda, err)
		}
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
		logger.Error(err, "Failed to replace image from params.env", "error", err.Error())
		return err
	}

	// Deploy odh-dashboard manifests
	if platform == deploy.OpenDataHub {
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
			Path,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			logger.Error(err, "Failed to set dashboard base config", "location", Path)
			return err
		}
	} else {
		// Apply authentication overlay
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathSupported,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			logger.Error(err, "Failed to set dashboard overĺay authentication", "location", ComponentNameSupported)
			return err
		}
	}

	// ISV handling
	switch platform {
	case deploy.SelfManagedRhods:
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathISVSM,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			logger.Error(err, "Failed to set dashboard ISV", "location", PathISVSM)
			return err
		}
		// ConsoleLink handling
		consoleRoute := &routev1.Route{}
		err = cli.Get(context.TODO(), client.ObjectKey{Name: NameConsoleLink, Namespace: NamespaceConsoleLink}, consoleRoute)
		if err != nil {
			logger.Error(err, "Failed to get console route URL", "name", NameConsoleLink, "namespace", NamespaceConsoleLink)
			return err
		}
		domainIndex := strings.Index(consoleRoute.Spec.Host, ".")
		consolelinkDomain := consoleRoute.Spec.Host[domainIndex+1:]
		err = common.ReplaceStringsInFile(PathConsoleLink, map[string]string{
			"<rhods-dashboard-url>": "https://rhods-dashboard-" + namespace + "." + consolelinkDomain,
		})
		if err != nil {
			logger.Error(err, "Failed to replace consolelink in manifests")
			return err
		}
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathConsoleLink,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			logger.Error(err, "Failed to set dashboard consolelink", "location", PathConsoleLink)
			return err
		}
		return err
	case deploy.ManagedRhods:
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathISVAddOn,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			return fmt.Errorf("Failed to set dashboard ISV from %s: %v", PathISVAddOn, err)
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
		})
		if err != nil {
			logger.Error(err, "Failed to replace consolelink in manifests")
			return err
		}
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentNameSupported,
			PathConsoleLink,
			namespace,
			scheme, enabled, logger)
		if err != nil {
			logger.Error(err, "Failed to set dashboardconsolelink", "location", PathConsoleLink)
			return err
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
