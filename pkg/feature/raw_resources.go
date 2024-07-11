package feature

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	yamlResourceSeparator = "(?m)^---[ \t]*$"
)

func applyResources(ctx context.Context, cli client.Client, objects []*unstructured.Unstructured, metaOptions ...cluster.MetaOptions) error {
	for _, source := range objects {
		target := source.DeepCopy()

		name := source.GetName()
		namespace := source.GetNamespace()

		errGet := cli.Get(ctx, k8stypes.NamespacedName{Name: name, Namespace: namespace}, target)
		if client.IgnoreNotFound(errGet) != nil {
			return fmt.Errorf("failed to get resource %s/%s: %w", namespace, name, errGet)
		}

		for _, opt := range metaOptions {
			if err := opt(target); err != nil {
				return err
			}
		}

		if k8serr.IsNotFound(errGet) {
			if errCreate := cli.Create(ctx, target); client.IgnoreAlreadyExists(errCreate) != nil {
				return fmt.Errorf("failed to create source %s/%s: %w", namespace, name, errCreate)
			}

			return nil
		}

		if shouldReconcile(source) {
			if errUpdate := patchUsingApplyStrategy(ctx, cli, source, target); errUpdate != nil {
				return fmt.Errorf("failed to reconcile resource %s/%s: %w", namespace, name, errUpdate)
			}
		}
	}

	return nil
}

func patchResources(ctx context.Context, cli client.Client, patches []*unstructured.Unstructured) error {
	for _, patch := range patches {
		if errPatch := patchUsingMergeStrategy(ctx, cli, patch); errPatch != nil {
			return errPatch
		}
	}

	return nil
}

// patchUsingApplyStrategy applies a server-side apply patch to a Kubernetes resource.
// It treats the provided source as the desired state of the resource and attempts to
// reconcile the target resource to match this state. The function takes ownership of the
// fields specified in the target and will ensure they match desired state.
func patchUsingApplyStrategy(ctx context.Context, cli client.Client, source, target *unstructured.Unstructured) error {
	data, errJSON := source.MarshalJSON()
	if errJSON != nil {
		return fmt.Errorf("error converting yaml to json: %w", errJSON)
	}
	return cli.Patch(ctx, target, client.RawPatch(k8stypes.ApplyPatchType, data), client.ForceOwnership, client.FieldOwner("rhods-operator"))
}

// patchUsingMergeStrategy merges the specified fields into the existing resources.
// Fields included in the patch will overwrite existing fields, while fields not included will remain unchanged.
func patchUsingMergeStrategy(ctx context.Context, cli client.Client, patch *unstructured.Unstructured) error {
	data, errJSON := patch.MarshalJSON()
	if errJSON != nil {
		return fmt.Errorf("error converting yaml to json: %w", errJSON)
	}

	if errPatch := cli.Patch(ctx, patch, client.RawPatch(k8stypes.MergePatchType, data)); errPatch != nil {
		return fmt.Errorf("failed patching resource: %w", errPatch)
	}

	return nil
}

// At this point we only look at what is defined for the resource in the desired state (source),
// so the one provided by the operator (e.g. embedded manifest files).
//
// Although the actual instance on the cluster (the target) might be in a different state,
// we intentionally do not address this scenario due to the lack of clear requirements
// on the extent to which users can modify it. Additionally, addressing this would
// necessitate extra reporting (e.g., via status.conditions on a FeatureTracker)
// to highlight discrepancies between the actual and desired state when users opt out
// of operator management/reconciliation.
func shouldReconcile(source *unstructured.Unstructured) bool {
	if isManaged(source) {
		return true
	}

	if isUnmanaged(source) {
		return false
	}

	// In all the other cases preserve original behaviour
	return false
}

func markAsManaged(objs []*unstructured.Unstructured) {
	for _, obj := range objs {
		objAnnotations := obj.GetAnnotations()
		if objAnnotations == nil {
			objAnnotations = make(map[string]string)
		}

		// If resource already has management mode defined, it should take precedence
		if _, exists := objAnnotations[annotations.ManagedByODHOperator]; !exists {
			objAnnotations[annotations.ManagedByODHOperator] = "true"
			obj.SetAnnotations(objAnnotations)
		}
	}
}

func isUnmanaged(obj *unstructured.Unstructured) bool {
	managed, isDefined := obj.GetAnnotations()[annotations.ManagedByODHOperator]
	return isDefined && managed == "false"
}

func isManaged(obj *unstructured.Unstructured) bool {
	managed, isDefined := obj.GetAnnotations()[annotations.ManagedByODHOperator]
	return isDefined && managed == "true"
}
