package deploy

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// removeOwnerReferences removes all owner references from a Kubernetes object that match the provided predicate.
//
// This function iterates through the OwnerReferences of the given object, filters out those that satisfy
// the predicate, and updates the object in the cluster using the provided client.
//
// Parameters:
//   - ctx: The context for the request, which can carry deadlines, cancellation signals, and other request-scoped values.
//   - cli: A controller-runtime client used to update the Kubernetes object.
//   - obj: The Kubernetes object whose OwnerReferences are to be filtered. It must implement client.Object.
//   - predicate: A function that takes an OwnerReference and returns true if the reference should be removed.
//
// Returns:
//   - An error if the update operation fails, otherwise nil.
func removeOwnerReferences(
	ctx context.Context,
	cli client.Client,
	obj client.Object,
	predicate func(reference metav1.OwnerReference) bool,
) error {
	oldRefs := obj.GetOwnerReferences()
	if len(oldRefs) == 0 {
		return nil
	}

	newRefs := oldRefs[:0]
	for _, ref := range oldRefs {
		if !predicate(ref) {
			newRefs = append(newRefs, ref)
		}
	}

	if len(newRefs) == len(oldRefs) {
		return nil
	}

	obj.SetOwnerReferences(newRefs)

	// Update the object in the cluster
	if err := cli.Update(ctx, obj); err != nil {
		return fmt.Errorf(
			"failed to remove owner references from object %s/%s with gvk %s: %w",
			obj.GetNamespace(),
			obj.GetName(),
			obj.GetObjectKind().GroupVersionKind(),
			err,
		)
	}

	return nil
}
