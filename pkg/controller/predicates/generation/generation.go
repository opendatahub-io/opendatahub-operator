package generation

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ predicate.Predicate = Predicate{}

type Predicate struct {
	predicate.Funcs
}

// Update implements default UpdateEvent filter for validating generation change.
func (Predicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	// If the generation is set to zero, it means that for such resource, the
	// generation does not matter, hence we should pass the event down for
	// further processing (if needed)
	if e.ObjectNew.GetGeneration() == 0 || e.ObjectOld.GetGeneration() == 0 {
		return true
	}

	return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
}

func New() *Predicate {
	return &Predicate{}
}
