package workbenches

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	metadataannotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	hardwareProfileNameAnnotation      = "opendatahub.io/hardware-profile-name"
	hardwareProfileNamespaceAnnotation = "opendatahub.io/hardware-profile-namespace"
	acceleratorNameAnnotation          = "opendatahub.io/accelerator-name"
	lastSizeSelectionAnnotation        = "notebooks.opendatahub.io/last-size-selection"
	oauthProxyContainerName            = "oauth-proxy"
	oauthProxyImageSubstring           = "ose-oauth-proxy-rhel9"
)

func Check(ctx context.Context, reader client.Reader, _, applicationNamespace string) error {
	notebooks, err := listNotebooks(ctx, reader)
	if err != nil {
		return err
	}

	brokenRefs, err := countBrokenHardwareProfileRefs(ctx, reader, notebooks)
	if err != nil {
		return err
	}
	brokenConnections, err := countBrokenConnectionRefs(ctx, reader, notebooks)
	if err != nil {
		return err
	}
	containerNameMismatches, err := countContainerNameMismatches(notebooks)
	if err != nil {
		return err
	}
	if brokenRefs == 0 && brokenConnections == 0 && containerNameMismatches == 0 {
		return nil
	}

	return &UpgradeBlockedError{
		NotebooksWithBrokenHardwareProfileRefs: brokenRefs,
		NotebooksWithBrokenConnectionRefs:      brokenConnections,
		NotebooksWithContainerNameMismatch:     containerNameMismatches,
	}
}

func listNotebooks(ctx context.Context, reader client.Reader) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.Notebook)

	err := reader.List(ctx, list)
	switch {
	case meta.IsNoMatchError(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("listing Notebooks: %w", err)
	default:
		return list.Items, nil
	}
}

func countBrokenHardwareProfileRefs(
	ctx context.Context,
	reader client.Reader,
	notebooks []unstructured.Unstructured,
) (int, error) {
	count := 0
	for i := range notebooks {
		hwpName, namespacesToCheck := hardwareProfileLookup(&notebooks[i])
		if hwpName == "" {
			continue
		}
		found := false
		for _, ns := range namespacesToCheck {
			exists, err := hardwareProfileExists(ctx, reader, hwpName, ns)
			if err != nil {
				return 0, err
			}
			if exists {
				found = true
				break
			}
		}
		if !found {
			count++
		}
	}

	return count, nil
}

func countBrokenConnectionRefs(
	ctx context.Context,
	reader client.Reader,
	notebooks []unstructured.Unstructured,
) (int, error) {
	cache := make(map[types.NamespacedName]bool)
	count := 0

	for i := range notebooks {
		refs := connectionRefs(&notebooks[i])
		if len(refs) == 0 {
			continue
		}

		for _, ref := range refs {
			exists, ok := cache[ref]
			if !ok {
				var err error
				exists, err = secretExists(ctx, reader, ref)
				if err != nil {
					return 0, err
				}
				cache[ref] = exists
			}
			if !exists {
				count++
				break
			}
		}
	}

	return count, nil
}

func countContainerNameMismatches(notebooks []unstructured.Unstructured) (int, error) {
	count := 0
	for i := range notebooks {
		mismatch, err := hasContainerNameMismatch(&notebooks[i])
		if err != nil {
			return 0, err
		}
		if mismatch {
			count++
		}
	}

	return count, nil
}

func hardwareProfileLookup(obj client.Object) (string, []string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return "", nil
	}

	hwpName := annotations[hardwareProfileNameAnnotation]
	if hwpName == "" {
		return "", nil
	}

	explicitNamespace := annotations[hardwareProfileNamespaceAnnotation]
	namespacesToCheck := make([]string, 0, 1)
	if explicitNamespace != "" {
		namespacesToCheck = append(namespacesToCheck, explicitNamespace)
	} else if obj.GetNamespace() != "" {
		namespacesToCheck = append(namespacesToCheck, obj.GetNamespace())
	}

	return hwpName, namespacesToCheck
}

func connectionRefs(obj client.Object) []types.NamespacedName {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	value := annotations[metadataannotations.Connection]
	if value == "" {
		return nil
	}

	refs := make([]types.NamespacedName, 0)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		ref := types.NamespacedName{Namespace: obj.GetNamespace()}
		if ns, name, hasSep := strings.Cut(part, "/"); hasSep {
			ref.Name = name
			if ns != "" {
				ref.Namespace = ns
			}
		} else if part != "" {
			ref.Name = part
		}

		if ref.Name != "" {
			refs = append(refs, ref)
		}
	}

	return refs
}

func hasDashboardAnnotation(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	return annotations[acceleratorNameAnnotation] != "" ||
		annotations[lastSizeSelectionAnnotation] != ""
}

type notebookContainer struct {
	Name  string
	Image string
}

func hasContainerNameMismatch(obj *unstructured.Unstructured) (bool, error) {
	if !hasDashboardAnnotation(obj) {
		return false, nil
	}

	containers, err := extractWorkloadContainers(obj)
	if err != nil {
		return false, err
	}
	if len(containers) != 1 {
		return false, nil
	}

	return containers[0].Name != obj.GetName(), nil
}

func extractWorkloadContainers(obj *unstructured.Unstructured) ([]notebookContainer, error) {
	rawContainers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if err != nil {
		return nil, fmt.Errorf("reading Notebook %s/%s containers: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	if !found {
		return nil, nil
	}

	containers := make([]notebookContainer, 0, len(rawContainers))
	for _, raw := range rawContainers {
		containerMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		name, _ := containerMap["name"].(string)
		image, _ := containerMap["image"].(string)
		if isInfrastructureContainer(name, image) {
			continue
		}

		containers = append(containers, notebookContainer{
			Name:  name,
			Image: image,
		})
	}

	return containers, nil
}

func isInfrastructureContainer(name string, image string) bool {
	return name == oauthProxyContainerName && strings.Contains(image, oauthProxyImageSubstring)
}

func hardwareProfileExists(
	ctx context.Context,
	reader client.Reader,
	hwpName string,
	namespace string,
) (bool, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.HardwareProfile)
	err := reader.Get(ctx, client.ObjectKey{Name: hwpName, Namespace: namespace}, obj)
	switch {
	case err == nil:
		return true, nil
	case meta.IsNoMatchError(err):
		return false, nil
	case k8serr.IsNotFound(err):
		return false, nil
	default:
		return false, fmt.Errorf("getting HardwareProfile %s/%s: %w", namespace, hwpName, err)
	}
}

func secretExists(
	ctx context.Context,
	reader client.Reader,
	ref types.NamespacedName,
) (bool, error) {
	secret := &corev1.Secret{}
	err := reader.Get(ctx, ref, secret)
	switch {
	case err == nil:
		return true, nil
	case k8serr.IsNotFound(err):
		return false, nil
	default:
		return false, fmt.Errorf("getting Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
}
