package components

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Component struct {
	// Set to "Managed" to enable the component, and to "Removed" to disable it.
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Add any other common fields across components below
}

type ComponentInterface interface {
	ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme,
		managementState operatorv1.ManagementState, namespace string) error
	GetComponentName() string
	SetImageParamsMap(imageMap map[string]string) map[string]string
}
