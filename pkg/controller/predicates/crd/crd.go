package crd

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func ServingCRD() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() != "inferenceservices.serving.kserve.io" && e.ObjectNew.GetName() != "servingruntimes.serving.kserve.io"
		},
	}
}
