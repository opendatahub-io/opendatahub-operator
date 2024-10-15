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
			labelList := e.Object.GetLabels()

			if v, exist := labelList[name]; exist && v == value {
				return true
			}

			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldLabels := e.ObjectOld.GetLabels()
			if v, exist := oldLabels[name]; exist && v == value {
				return true
			}

			newLabels := e.ObjectNew.GetLabels()
			if v, exist := newLabels[name]; exist && v == value {
				return true
			}

			return false
		},
	}
}
