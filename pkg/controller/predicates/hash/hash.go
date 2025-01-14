package hash

import (
	"bytes"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// Updated is a watch predicate that can be used to ignore updates
// of resources if they're considered equal after hashing by resources.Hash().
func Updated() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldUnstructured, err := resources.ToUnstructured(e.ObjectOld.DeepCopyObject())
			if err != nil {
				return true
			}
			newUnstructured, err := resources.ToUnstructured(e.ObjectNew.DeepCopyObject())
			if err != nil {
				return true
			}

			oldHash, err := resources.Hash(oldUnstructured)
			if err != nil {
				return true
			}
			newHash, err := resources.Hash(newUnstructured)
			if err != nil {
				return true
			}

			return !bytes.Equal(oldHash, newHash)
		},
	}
}
