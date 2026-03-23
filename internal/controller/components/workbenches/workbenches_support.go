package workbenches

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.WorkbenchesComponentName

	ReadyConditionType = componentApi.WorkbenchesKind + status.ReadySuffix

	notebooksPath = "notebooks"

	notebookControllerPath               = "odh-notebook-controller"
	notebookControllerManifestSourcePath = "base"

	kfNotebookControllerPath               = "kf-notebook-controller"
	kfNotebookControllerManifestSourcePath = "overlays/openshift"

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "workbenches"
)

var (
	sectionTitle = map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
	}

	notebookControllerContextDir   = path.Join(ComponentName, notebookControllerPath)
	kfNotebookControllerContextDir = path.Join(ComponentName, kfNotebookControllerPath)
	notebookContextDir             = path.Join(ComponentName, notebooksPath)

	notebookImagesManifestSourcePath = map[common.Platform]string{
		cluster.SelfManagedRhoai: "rhoai/overlays/additional",
		cluster.ManagedRhoai:     "rhoai/overlays/additional",
		cluster.OpenDataHub:      "odh/overlays/additional",
	}

	notebookImagesParamsPath = map[common.Platform]string{
		cluster.SelfManagedRhoai: "rhoai/base",
		cluster.ManagedRhoai:     "rhoai/base",
		cluster.OpenDataHub:      "odh/base",
	}
)

var (
	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

// manifests for nbc in ODH and RHOAI + downstream use it for imageparams.
func notebookControllerManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: notebookControllerContextDir,
		SourcePath: sourcePath,
	}
}

// manifests for ODH nbc + downstream use it for imageparams.
func kfNotebookControllerManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: kfNotebookControllerContextDir,
		SourcePath: sourcePath,
	}
}

// notebook image manifests.
func notebookImagesManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: notebookContextDir,
		SourcePath: sourcePath,
	}
}

func ComputeKustomizeVariable(ctx context.Context, cli client.Client, platform common.Platform) (map[string]string, error) {
	mlflowEnabled, err := isMLflowEnabled(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error checking MLflow status: %w", err)
	}

	title, ok := sectionTitle[platform]
	if !ok {
		title = sectionTitle[cluster.SelfManagedRhoai]
	}

	consoleLinkDomain, err := gateway.GetGatewayDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting gateway domain: %w", err)
	}

	// When gateway domain is empty (e.g., Gateway not ready yet), we return an empty
	// gateway-url instead of failing. This allows odh-notebook-controller to use its
	// fallback mechanism while still receiving other values like mlflow-enabled.
	gatewayURL := ""
	if consoleLinkDomain != "" {
		if strings.ContainsAny(consoleLinkDomain, "\n\r=") {
			return nil, fmt.Errorf("invalid gateway domain %q: contains illegal characters", consoleLinkDomain)
		}
		gatewayURL = consoleLinkDomain
	}

	return map[string]string{
		"gateway-url":    gatewayURL,
		"section-title":  title,
		"mlflow-enabled": strconv.FormatBool(mlflowEnabled),
	}, nil
}

func isMLflowEnabled(ctx context.Context, cli client.Client) (bool, error) {
	dsc, err := cluster.GetDSC(ctx, cli)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get DataScienceCluster: %w", err)
	}

	return dsc.Spec.Components.MLflowOperator.ManagementState == operatorv1.Managed, nil
}
