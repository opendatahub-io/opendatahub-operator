package kueue

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
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

	// Kueue managed namespace annotation keys.
	KueueManagedAnnotationKey       = "kueue.openshift.io/managed"
	KueueLegacyManagedAnnotationKey = "kueue-managed"

	KueueConfigCRName   = "cluster"
	KueueConfigMapName  = "kueue-manager-config"
	KueueConfigMapEntry = "controller_manager_config.yaml"

	NSListLimit = 500
)

var (
	imageParamMap = map[string]string{
		"odh-kueue-controller-image": "RELATED_IMAGE_ODH_KUEUE_CONTROLLER_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
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
		KueueManagedAnnotationKey: "true",
	}); err != nil {
		return nil, err
	}
	// Add namespaces with legacy management label.
	if err := collectNamespacesWithPagination(ctx, c, uniqueNamespaces, client.MatchingLabels{
		KueueLegacyManagedAnnotationKey: "true",
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
			return fmt.Errorf("failed to list namespaces with label %s: %w", KueueManagedAnnotationKey+"=true", err)
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

// i.e. if a namespace has just the KueueLegacyManagedAnnotationKey or the KueueManagedAnnotationKey, the other one is added as well.
func fixAnnotationsOfManagedNamespaces(ctx context.Context, c client.Client, namespaces []corev1.Namespace) error {
	for _, ns := range namespaces {
		hasLegacy := resources.HasLabel(&ns, KueueLegacyManagedAnnotationKey)
		hasManaged := resources.HasLabel(&ns, KueueManagedAnnotationKey)

		// Skip if both labels are already present
		if hasLegacy && hasManaged {
			continue
		}

		// Set both labels to ensure consistency
		resources.SetLabels(&ns, map[string]string{
			KueueLegacyManagedAnnotationKey: "true",
			KueueManagedAnnotationKey:       "true",
		})

		if err := c.Update(ctx, &ns); err != nil {
			if !k8serr.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func createDefaultClusterQueue(name string) *unstructured.Unstructured {
	clusterQueue := &unstructured.Unstructured{}

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
			"namespaceSelector": map[string]interface{}{},
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
