package generation

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ predicate.Predicate = Predicate{}

type Predicate struct {
	predicate.Funcs
}

func (Predicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	if e.ObjectNew.GetGeneration() == 0 || e.ObjectOld.GetGeneration() == 0 {
		return true
	}

	return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
}

func New() *Predicate {
	return &Predicate{}
}
