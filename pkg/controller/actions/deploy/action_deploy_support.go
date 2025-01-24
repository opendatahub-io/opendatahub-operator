package deploy

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func isLegacyOwnerRef(or metav1.OwnerReference) bool {
	switch {
	case or.APIVersion == gvk.DataScienceCluster.GroupVersion().String() && or.Kind == gvk.DataScienceCluster.Kind:
		return true
	case or.APIVersion == gvk.DSCInitialization.GroupVersion().String() && or.Kind == gvk.DSCInitialization.Kind:
		return true
	default:
		return false
	}
}
