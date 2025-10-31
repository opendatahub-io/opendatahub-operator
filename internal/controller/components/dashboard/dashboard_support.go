package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	ComponentName = componentApi.DashboardComponentName

	ReadyConditionType = componentApi.DashboardKind + status.ReadySuffix

	// Legacy component names are the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.

	LegacyComponentNameUpstream   = "dashboard"
	LegacyComponentNameDownstream = "rhods-dashboard"

	// Deployment names based on platform.
	DeploymentNameODH   = "odh-dashboard"
	DeploymentNameRhoai = "rhods-dashboard"

	// ConfigMap names for dashboard parameters based on platform.
	ConfigMapNameODHParams   = "odh-dashboard-params"
	ConfigMapNameRhoaiParams = "rhoai-dashboard-params"

	// Dashboard path on the gateway.
	dashboardPath = "/"
)

var (
	sectionTitle = map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
	}

	overlaysSourcePaths = map[common.Platform]string{
		cluster.SelfManagedRhoai: "/rhoai/onprem",
		cluster.ManagedRhoai:     "/rhoai/addon",
		cluster.OpenDataHub:      "/odh",
	}

	imagesMap = map[string]string{
		"odh-dashboard-image":     "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"model-registry-ui-image": "RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
		"gen-ai-ui-image":         "RELATED_IMAGE_ODH_MOD_ARCH_GEN_AI_IMAGE",
		"kube-rbac-proxy":         "RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func defaultManifestInfo(p common.Platform) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: overlaysSourcePaths[p],
	}
}

func bffManifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "modular-architecture",
	}
}

func computeKustomizeVariable(ctx context.Context, cli client.Client, platform common.Platform) (map[string]string, error) {
	gatewayDomain, err := gateway.GetGatewayDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting gateway domain: %w", err)
	}

	return map[string]string{
		"dashboard-url": fmt.Sprintf("https://%s%s", gatewayDomain, dashboardPath),
		"section-title": sectionTitle[platform],
	}, nil
}

// hashWithGatewayConfig computes a cache key that includes GatewayConfig generation
// to ensure cache invalidation when GatewayConfig domain/subdomain changes.
// This function matches the cacher.CachingKeyFn signature and can be used as a custom cache key.
func hashWithGatewayConfig(rr *odhtypes.ReconciliationRequest) ([]byte, error) {
	// Start with the standard hash
	baseHash, err := odhtypes.Hash(rr)
	if err != nil {
		return nil, fmt.Errorf("failed to compute base hash: %w", err)
	}

	// Get GatewayConfig to include its generation in the hash
	// GatewayConfig changes affect params.env which changes the generated ConfigMap
	gatewayConfig := &serviceApi.GatewayConfig{}
	if err := rr.Client.Get(context.Background(), client.ObjectKey{Name: serviceApi.GatewayInstanceName}, gatewayConfig); err != nil {
		// GatewayConfig doesn't exist yet (IsNotFound) or other error
		// Use base hash - this handles the initial deployment case
		return baseHash, nil //nolint:nilerr
	}

	// GatewayConfig exists, include its generation in the hash
	// Use fixed 8-byte encoding for efficiency and consistency
	// Generation is always positive and safe to convert to uint64
	generationBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(generationBytes, uint64(gatewayConfig.GetGeneration())) //nolint:gosec // Generation is always positive int64

	// Combine baseHash and generation efficiently
	hash := sha256.New()
	hash.Write(baseHash)
	hash.Write(generationBytes)

	return hash.Sum(nil), nil
}

func computeComponentName() string {
	release := cluster.GetRelease()

	if isRhoaiPlatform(release.Name) {
		return LegacyComponentNameDownstream
	}
	return LegacyComponentNameUpstream
}

// isRhoaiPlatform returns true if the platform is SelfManagedRhoai or ManagedRhoai.
func isRhoaiPlatform(platform common.Platform) bool {
	return platform == cluster.SelfManagedRhoai || platform == cluster.ManagedRhoai
}

// getDeploymentName returns the deployment name based on the platform.
func getDeploymentName(platform common.Platform) string {
	if isRhoaiPlatform(platform) {
		return DeploymentNameRhoai
	}
	return DeploymentNameODH
}

// getConfigMapName returns the ConfigMap name for dashboard parameters based on the platform.
func getConfigMapName(platform common.Platform) string {
	if isRhoaiPlatform(platform) {
		return ConfigMapNameRhoaiParams
	}
	return ConfigMapNameODHParams
}

// getIndexedResource, replaceResourceAtIndex, and hashConfigMap are duplicated from kserve
// TODO: Consider extracting to a shared utility package in the future.
func getIndexedResource(rs []unstructured.Unstructured, obj any, g schema.GroupVersionKind, name string) (int, error) {
	var idx = -1
	for i, r := range rs {
		if r.GroupVersionKind() == g && r.GetName() == name {
			idx = i
			break
		}
	}

	if idx == -1 {
		return -1, fmt.Errorf("could not find %T with name %v in resources list", obj, name)
	}

	err := runtime.DefaultUnstructuredConverter.FromUnstructured(rs[idx].Object, obj)
	if err != nil {
		return idx, fmt.Errorf("failed converting to %T from resource %s: %w", obj, resources.FormatObjectReference(&rs[idx]), err)
	}

	return idx, nil
}

func replaceResourceAtIndex(rs []unstructured.Unstructured, idx int, obj any) error {
	u, err := resources.ToUnstructured(obj)
	if err != nil {
		return err
	}

	rs[idx] = *u
	return nil
}

func hashConfigMap(cm *corev1.ConfigMap) (string, error) {
	u, err := resources.ToUnstructured(cm)
	if err != nil {
		return "", err
	}

	h, err := resources.Hash(u)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(h), nil
}
