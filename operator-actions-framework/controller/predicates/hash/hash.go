package hash

import (
	"bytes"

	"github.com/opendatahub-io/operator-actions-framework/resources"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

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
