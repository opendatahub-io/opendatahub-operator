package dependent

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var _ predicate.Predicate = Predicate{}

type PredicateOption func(*Predicate) *Predicate

func WithWatchDeleted(val bool) PredicateOption {
	return func(in *Predicate) *Predicate {
		in.WatchDelete = val
		return in
	}
}

func WithWatchUpdate(val bool) PredicateOption {
	return func(in *Predicate) *Predicate {
		in.WatchUpdate = val
		return in
	}
}

func WithWatchStatus(val bool) PredicateOption {
	return func(in *Predicate) *Predicate {
		in.WatchStatus = val
		return in
	}
}

func New(opts ...PredicateOption) *Predicate {
	dp := &Predicate{
		WatchDelete: true,
		WatchUpdate: true,
		WatchStatus: false,
	}

	for i := range opts {
		dp = opts[i](dp)
	}

	return dp
}

type Predicate struct {
	WatchDelete bool
	WatchUpdate bool
	WatchStatus bool

	predicate.Funcs
}

func (p Predicate) Create(event.CreateEvent) bool {
	return false
}

func (p Predicate) Generic(event.GenericEvent) bool {
	return false
}

func (p Predicate) Delete(e event.DeleteEvent) bool {
	return p.WatchDelete
}

func (p Predicate) Update(e event.UpdateEvent) bool {
	if !p.WatchUpdate {
		return false
	}

	if e.ObjectOld.GetResourceVersion() == e.ObjectNew.GetResourceVersion() {
		return false
	}

	oldObj, err := resources.ToUnstructured(e.ObjectOld)
	if err != nil {
		return false
	}

	newObj, err := resources.ToUnstructured(e.ObjectNew)
	if err != nil {
		return false
	}

	oldObj = oldObj.DeepCopy()
	newObj = newObj.DeepCopy()

	if !p.WatchStatus {
		// Update filters out events that change only the dependent resource
		// status. It is not typical for the controller of a primary
		// resource to write to the status of one its dependent resources.
		unstructured.RemoveNestedField(oldObj.Object, "status")
		unstructured.RemoveNestedField(newObj.Object, "status")
	}

	// Reset field not meaningful for comparison
	oldObj.SetResourceVersion("")
	newObj.SetResourceVersion("")
	oldObj.SetManagedFields(nil)
	newObj.SetManagedFields(nil)

	return !reflect.DeepEqual(oldObj.Object, newObj.Object)
}
