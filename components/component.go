package components

import (
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Component struct {
	// Set to "true" to enable component, "false" to disable component. A disabled component will not be installed.
	Enabled bool `json:"enabled"`
	// Add any other common fields across components below
}

type ComponentInterface interface {
	ReconcileComponent(
		owner metav1.Object,
		client client.Client,
		scheme *runtime.Scheme,
		enabled bool,
		namespace string,
		logger logr.Logger,
	) error
	GetComponentName() string
	SetImageParamsMap(imageMap map[string]string) map[string]string
}
