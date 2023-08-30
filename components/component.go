package components

import (
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Component struct {
	// Set to one of the following values:
	//
	// - "Managed" : the operator is actively managing the component and trying to keep it active.
	//               It will only upgrade the component if it is safe to do so
	//
	// - "Removed" : the operator is actively managing the component and will not install it,
	//               or if it is installed, the operator will try to remove it
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Add any other common fields across components below
}

func (c *Component) GetManagementState() operatorv1.ManagementState {
	return c.ManagementState
}

// DataScienceClusterConfig passing Spec of DSCI for reconcile DataScienceCluster
type DataScienceClusterConfig struct {
	DSCISpec *dsci.DSCInitializationSpec
	Platform dsci.Platform
}

type ComponentInterface interface {
	ReconcileComponent(cli client.Client, owner metav1.Object, dsciInfo *DataScienceClusterConfig) error
	GetComponentName() string
	GetManagementState() operatorv1.ManagementState
	SetImageParamsMap(imageMap map[string]string) map[string]string
}
