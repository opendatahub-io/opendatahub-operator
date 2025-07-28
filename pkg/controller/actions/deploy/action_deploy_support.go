package deploy

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func isLegacyOwnerRef(or metav1.OwnerReference) bool {
	switch {
	case or.APIVersion == gvk.DataScienceCluster.GroupVersion().String() && or.Kind == gvk.DataScienceCluster.Kind:
		return true
	case or.APIVersion == gvk.DSCInitialization.GroupVersion().String() && or.Kind == gvk.DSCInitialization.Kind:
		return true
	case or.APIVersion == gvk.FeatureTracker.GroupVersion().String() && or.Kind == gvk.FeatureTracker.Kind:
		return true
	default:
		return false
	}
}

func ownedTypeIsNot(ownerType *schema.GroupVersionKind) func(or metav1.OwnerReference) bool {
	if ownerType == nil {
		return func(or metav1.OwnerReference) bool {
			return false
		}
	}

	gv := ownerType.GroupVersion().String()
	return func(or metav1.OwnerReference) bool {
		return ownerType.Kind != or.Kind && gv != or.APIVersion
	}
}

func ownedTypeIs(ownerType *schema.GroupVersionKind) func(or metav1.OwnerReference) bool {
	if ownerType == nil {
		return func(or metav1.OwnerReference) bool {
			return false
		}
	}

	gv := ownerType.GroupVersion().String()

	return func(or metav1.OwnerReference) bool {
		return ownerType.Kind == or.Kind && gv == or.APIVersion
	}
}
