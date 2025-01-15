package clusterrole

import (
	"reflect"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// IgnoreIfAggregationRule is a watch predicate that can be used with
// ClusterRoles to ignore the rules field on update if aggregationRule is set.
func IgnoreIfAggregationRule() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldClusterRole, ok := e.ObjectOld.DeepCopyObject().(*rbacv1.ClusterRole)
			if !ok {
				return true
			}
			newClusterRole, ok := e.ObjectNew.DeepCopyObject().(*rbacv1.ClusterRole)
			if !ok {
				return true
			}

			// if aggregationRule is set, then the rules are set by k8s based on other
			// ClusterRoles matching a label selector, so we shouldn't try to reset that
			// back to empty
			if newClusterRole.AggregationRule != nil {
				oldClusterRole.Rules = nil
				newClusterRole.Rules = nil
			}

			oldClusterRole.SetManagedFields(nil)
			newClusterRole.SetManagedFields(nil)
			oldClusterRole.SetResourceVersion("")
			newClusterRole.SetResourceVersion("")

			return !reflect.DeepEqual(oldClusterRole, newClusterRole)
		},
	}
}
