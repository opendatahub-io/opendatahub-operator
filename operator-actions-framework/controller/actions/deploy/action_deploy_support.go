package deploy

import (
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	DeploymentGVK = schema.GroupVersionKind{
		Group:   appsv1.SchemeGroupVersion.Group,
		Version: appsv1.SchemeGroupVersion.Version,
		Kind:    "Deployment",
	}
	ClusterRoleGVK = schema.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "ClusterRole",
	}
)

func ownedTypeIsNot(ownerType *schema.GroupVersionKind) func(or metav1.OwnerReference) bool {
	if ownerType == nil {
		return func(or metav1.OwnerReference) bool {
			return false
		}
	}

	gv := ownerType.GroupVersion().String()
	return func(or metav1.OwnerReference) bool {
		return ownerType.Kind != or.Kind || gv != or.APIVersion
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
