package partial

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ predicate.Predicate = Predicate{}

type PredicateOption func(*Predicate) *Predicate

func WatchDeleted(val bool) PredicateOption {
	return func(in *Predicate) *Predicate {
		in.WatchDelete = val
		return in
	}
}

func WatchUpdate(val bool) PredicateOption {
	return func(in *Predicate) *Predicate {
		in.WatchUpdate = val
		return in
	}
}

func New(opts ...PredicateOption) *Predicate {
	dp := &Predicate{
		WatchDelete: true,
		WatchUpdate: true,
	}

	for i := range opts {
		dp = opts[i](dp)
	}

	return dp
}

type Predicate struct {
	WatchDelete bool
	WatchUpdate bool

	predicate.Funcs
}

func (p Predicate) Create(event.CreateEvent) bool {
	return false
}

func (p Predicate) Generic(event.GenericEvent) bool {
	return false
}

func (p Predicate) Delete(e event.DeleteEvent) bool {
	if !p.WatchDelete {
		return false
	}

	_, ok := e.Object.(*metav1.PartialObjectMetadata)

	return ok
}

func (p Predicate) Update(e event.UpdateEvent) bool {
	if !p.WatchUpdate {
		return false
	}

	if e.ObjectOld.GetResourceVersion() == e.ObjectNew.GetResourceVersion() {
		return false
	}

	_, ok := e.ObjectNew.(*metav1.PartialObjectMetadata)

	return ok
}
