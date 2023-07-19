package components

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Component struct {
	// enables or disables the component. A disabled component will not be installed.
	Enabled bool `json:"enabled"`
	// Add any other common fields across components below
}

type ComponentInterface interface {
	ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme,
		enabled bool, namespace string) error
}
