package component

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func ForLabel(name string, value string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			values := e.Object.GetLabels()

			if v, exist := values[name]; exist && v == value {
				return true
			}

			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldValues := e.ObjectOld.GetLabels()
			if v, exist := oldValues[name]; exist && v == value {
				return true
			}

			newValues := e.ObjectNew.GetLabels()
			if v, exist := newValues[name]; exist && v == value {
				return true
			}

			return false
		},
	}
}

func ForAnnotation(name string, value string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			values := e.Object.GetAnnotations()

			if v, exist := values[name]; exist && v == value {
				return true
			}

			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldValues := e.ObjectOld.GetAnnotations()
			if v, exist := oldValues[name]; exist && v == value {
				return true
			}

			newValues := e.ObjectNew.GetAnnotations()
			if v, exist := newValues[name]; exist && v == value {
				return true
			}

			return false
		},
	}
}
