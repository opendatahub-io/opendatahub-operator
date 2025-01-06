package component

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func ForLabel(name string, value string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return resources.HasLabel(e.Object, name, value)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return resources.HasLabel(e.ObjectNew, name, value) || resources.HasLabel(e.ObjectOld, name, value)
		},
	}
}

func ForAnnotation(name string, value string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return resources.HasAnnotation(e.Object, name, value)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return resources.HasAnnotation(e.ObjectNew, name, value) || resources.HasAnnotation(e.ObjectOld, name, value)
		},
	}
}

func ForLabelAllEvents(name string, value string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return resources.HasLabel(e.Object, name, value)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return resources.HasLabel(e.Object, name, value)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return resources.HasLabel(e.ObjectNew, name, value) || resources.HasLabel(e.ObjectOld, name, value)
		},
	}
}
