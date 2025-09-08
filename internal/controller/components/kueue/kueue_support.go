package kueue

import (
	"context"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	ComponentName = componentApi.KueueComponentName

	ReadyConditionType = componentApi.KueueKind + status.ReadySuffix

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName        = "kueue"
	ClusterQueueViewerRoleName = "kueue-clusterqueue-viewer-role"
	KueueBatchUserLabel        = "rbac.kueue.x-k8s.io/batch-user"
	KueueAdminRoleBindingName  = "kueue-admin-rolebinding"
	KueueAdminRoleName         = "kueue-batch-admin-role"

	KueueCRName         = "cluster"
	KueueConfigMapName  = "kueue-manager-config"
	KueueConfigMapEntry = "controller_manager_config.yaml"

	NSListLimit = 500

	// GPU resource keys.
	NvidiaGPUResourceKey = "nvidia.com/gpu"
	AMDGPUResourceKey    = "amd.com/gpu"
	// Flavor names.
	DefaultFlavorName = "default-flavor"
	NvidiaFlavorName  = "nvidia-gpu-flavor"
	AMDFlavorName     = "amd-gpu-flavor"
)

var (
	imageParamMap = map[string]string{
		"odh-kueue-controller-image": "RELATED_IMAGE_ODH_KUEUE_CONTROLLER_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}

	supportedGPUMap = map[string]string{
		NvidiaGPUResourceKey: NvidiaFlavorName,
		AMDGPUResourceKey:    AMDFlavorName,
	}
)

func manifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "rhoai",
	}
}

func kueueConfigManifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: "kueue-configs",
		SourcePath: "",
	}
}

func getManagedNamespaces(ctx context.Context, c client.Client) ([]corev1.Namespace, error) {
	// Deduped namespaces, since some might have both labels.
	var uniqueNamespaces = make(map[string]corev1.Namespace)

	// Add all namespaces with management label.
	if err := collectNamespacesWithPagination(ctx, c, uniqueNamespaces, client.MatchingLabels{
		cluster.KueueManagedLabelKey: "true",
	}); err != nil {
		return nil, err
	}
	// Add namespaces with legacy management label.
	if err := collectNamespacesWithPagination(ctx, c, uniqueNamespaces, client.MatchingLabels{
		cluster.KueueLegacyManagedLabelKey: "true",
	}); err != nil {
		return nil, err
	}

	managedNamespacesList := []corev1.Namespace{}
	for v := range maps.Values(uniqueNamespaces) {
		managedNamespacesList = append(managedNamespacesList, v)
	}

	return managedNamespacesList, nil
}

func collectNamespacesWithPagination(ctx context.Context, c client.Client, namespaceSet map[string]corev1.Namespace, opts ...client.ListOption) error {
	lo := client.ListOptions{
		Limit: NSListLimit,
	}
	opts = append(opts, &lo)
	for {
		// Listing namespaces with management label
		namespaces := &corev1.NamespaceList{}
		if err := c.List(ctx, namespaces, opts...); err != nil {
			return fmt.Errorf("failed to list namespaces with label %s: %w", cluster.KueueManagedLabelKey+"=true", err)
		}

		for _, ns := range namespaces.Items {
			namespaceSet[ns.Name] = ns
		}

		if namespaces.Continue == "" {
			break
		}

		lo.Continue = namespaces.Continue
	}
	return nil
}

// i.e. if a namespace has just the KueueLegacyManagedLabelKey or the KueueManagedLabelKey, the other one is added as well.
func ensureKueueLabelsOnManagedNamespaces(ctx context.Context, c client.Client, namespaces []corev1.Namespace) error {
	for _, ns := range namespaces {
		hasLegacy := resources.HasLabel(&ns, cluster.KueueLegacyManagedLabelKey)
		hasManaged := resources.HasLabel(&ns, cluster.KueueManagedLabelKey)

		// Skip if both labels are already present
		if hasLegacy && hasManaged {
			continue
		}

		// Set both labels to ensure consistency
		resources.SetLabels(&ns, map[string]string{
			cluster.KueueLegacyManagedLabelKey: "true",
			cluster.KueueManagedLabelKey:       "true",
		})

		if err := c.Update(ctx, &ns); err != nil {
			if !k8serr.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

type FlavorResource struct {
	Name  string
	Value string
}

type Flavors struct {
	Name      string
	Resources []FlavorResource
}

func createResourceGroup(flavors []Flavors) map[string]interface{} {
	resourceMap := make(map[string]any)
	groupFlavors := make([]any, 0, len(flavors))

	for _, flavor := range flavors {
		resources := make([]any, 0, len(flavor.Resources))

		for _, resource := range flavor.Resources {
			resources = append(resources, map[string]any{
				"name":         resource.Name,
				"nominalQuota": resource.Value,
			})

			resourceMap[resource.Name] = true
		}

		groupFlavor := map[string]any{
			"name":      flavor.Name,
			"resources": resources,
		}
		groupFlavors = append(groupFlavors, groupFlavor)
	}

	resourceKeys := slices.Sorted(maps.Keys(resourceMap))
	coveredResources := make([]any, len(resourceKeys))
	for i, key := range resourceKeys {
		coveredResources[i] = key
	}

	return map[string]any{
		"coveredResources": coveredResources,
		"flavors":          groupFlavors,
	}
}

func createDefaultClusterQueue(name string, clusterInfo ClusterResourceInfo) *unstructured.Unstructured {
	clusterQueue := &unstructured.Unstructured{}

	resourceGroups := []any{
		createResourceGroup([]Flavors{
			{
				Name: DefaultFlavorName,
				Resources: []FlavorResource{
					{Name: "cpu", Value: clusterInfo.CPU.Allocatable.String()},
					{Name: "memory", Value: clusterInfo.Memory.Allocatable.String()},
				},
			},
		}),
	}

	clusterGPU := slices.Sorted(maps.Keys(clusterInfo.GPUInfo))
	for _, label := range clusterGPU {
		gpuInfo := clusterInfo.GPUInfo[label]
		resourceGroups = append(resourceGroups, createResourceGroup([]Flavors{
			{
				Name: supportedGPUMap[label],
				Resources: []FlavorResource{
					{Name: label, Value: gpuInfo.Allocatable.String()},
				},
			},
		}))
	}

	clusterQueue.Object = map[string]interface{}{
		"apiVersion": gvk.ClusterQueue.GroupVersion().String(),
		"kind":       gvk.ClusterQueue.Kind,
		"metadata": map[string]interface{}{
			"name": name,
			"annotations": map[string]interface{}{
				annotations.ManagedByODHOperator: "false",
			},
		},
		"spec": map[string]interface{}{
			"namespaceSelector": map[string]interface{}{
				"matchLabels": map[string]interface{}{
					cluster.KueueManagedLabelKey: "true",
				},
			},
			"resourceGroups": resourceGroups,
		},
	}

	return clusterQueue
}

func createDefaultLocalQueue(name string, clusterQueueName string, namespace string) *unstructured.Unstructured {
	localQueue := &unstructured.Unstructured{}

	localQueue.Object = map[string]interface{}{
		"apiVersion": gvk.LocalQueue.GroupVersion().String(),
		"kind":       gvk.LocalQueue.Kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"annotations": map[string]interface{}{
				annotations.ManagedByODHOperator: "false",
			},
		},
		"spec": map[string]interface{}{
			"clusterQueue": clusterQueueName,
		},
	}

	return localQueue
}

func createDefaultResourceFlavors(clusterInfo ClusterResourceInfo) []unstructured.Unstructured {
	resourceFlavors := []unstructured.Unstructured{}

	resourceFlavors = append(resourceFlavors, unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gvk.ResourceFlavor.GroupVersion().String(),
			"kind":       gvk.ResourceFlavor.Kind,
			"metadata": map[string]any{
				"name": DefaultFlavorName,
				"annotations": map[string]any{
					annotations.ManagedByODHOperator: "false",
				},
			},
			"spec": map[string]any{},
		},
	})

	for label := range clusterInfo.GPUInfo {
		resourceFlavor := unstructured.Unstructured{}
		resourceFlavor.Object = map[string]any{
			"apiVersion": gvk.ResourceFlavor.GroupVersion().String(),
			"kind":       gvk.ResourceFlavor.Kind,
			"metadata": map[string]any{
				"name": supportedGPUMap[label],
				"annotations": map[string]any{
					annotations.ManagedByODHOperator: "false",
				},
			},
			"spec": map[string]any{},
		}

		resourceFlavors = append(resourceFlavors, resourceFlavor)
	}

	return resourceFlavors
}

// ClusterResourceInfo contains information about a node's resources and GPU capabilities.
type ClusterResourceInfo struct {
	CPU     ResourceQuantity
	Memory  ResourceQuantity
	GPUInfo map[string]*ResourceQuantity
}

// extractGPUInfo extracts GPU information from a node's allocatable and capacity resources.
func (info *ClusterResourceInfo) extractGPUInfo(node corev1.Node) {
	if info.GPUInfo == nil {
		info.GPUInfo = make(map[string]*ResourceQuantity)
	}

	for resourceKey := range supportedGPUMap {
		value, ok := node.Status.Allocatable[corev1.ResourceName(resourceKey)]
		if !ok {
			continue
		}

		entry := info.GPUInfo[resourceKey]
		if entry == nil {
			entry = &ResourceQuantity{}
		}

		entry.Allocatable.Add(value)
		info.GPUInfo[resourceKey] = entry
	}
}

// ResourceQuantity represents a resource with its allocatable values.
type ResourceQuantity struct {
	Allocatable resource.Quantity
}

// getClusterNodes retrieves information about all cluster nodes including GPU resources.
func getClusterResourceInfo(ctx context.Context, c client.Client) (ClusterResourceInfo, error) {
	nodeList := &corev1.NodeList{}
	if err := c.List(ctx, nodeList); err != nil {
		return ClusterResourceInfo{}, fmt.Errorf("failed to list cluster nodes: %w", err)
	}

	info := ClusterResourceInfo{}

	for _, node := range nodeList.Items {
		info.CPU.Allocatable.Add(*node.Status.Allocatable.Cpu())
		info.Memory.Allocatable.Add(*node.Status.Allocatable.Memory())
		info.extractGPUInfo(node)
	}

	return info, nil
}
