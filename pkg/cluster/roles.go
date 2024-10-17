package cluster

import (
	"context"

	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateOrUpdateClusterRole creates cluster role based on define PolicyRules and optional metadata fields and updates the rules if it already exists.
func CreateOrUpdateClusterRole(ctx context.Context, cli client.Client, name string, rules []rbacv1.PolicyRule, metaOptions ...MetaOptions) (*rbacv1.ClusterRole, error) {
	desiredClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: rules,
	}

	if err := ApplyMetaOptions(desiredClusterRole, metaOptions...); err != nil {
		return nil, err
	}

	foundClusterRole := &rbacv1.ClusterRole{}
	err := cli.Get(ctx, client.ObjectKey{Name: desiredClusterRole.GetName()}, foundClusterRole)
	if k8serr.IsNotFound(err) {
		return desiredClusterRole, cli.Create(ctx, desiredClusterRole)
	}

	if err := ApplyMetaOptions(foundClusterRole, metaOptions...); err != nil {
		return nil, err
	}
	foundClusterRole.Rules = rules

	return foundClusterRole, cli.Update(ctx, foundClusterRole)
}

// DeleteClusterRole simply calls delete on a ClusterRole with the given name. Any error is returned. Check for IsNotFound.
func DeleteClusterRole(ctx context.Context, cli client.Client, name string) error {
	desiredClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return cli.Delete(ctx, desiredClusterRole)
}

// CreateOrUpdateClusterRoleBinding creates cluster role bindings based on define PolicyRules and optional metadata fields and updates the bindings if it already exists.
func CreateOrUpdateClusterRoleBinding(ctx context.Context, cli client.Client, name string,
	subjects []rbacv1.Subject, roleRef rbacv1.RoleRef,
	metaOptions ...MetaOptions) (*rbacv1.ClusterRoleBinding, error) {
	desiredClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: subjects,
		RoleRef:  roleRef,
	}

	if err := ApplyMetaOptions(desiredClusterRoleBinding, metaOptions...); err != nil {
		return nil, err
	}

	foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err := cli.Get(ctx, client.ObjectKey{Name: desiredClusterRoleBinding.GetName()}, foundClusterRoleBinding)
	if k8serr.IsNotFound(err) {
		return desiredClusterRoleBinding, cli.Create(ctx, desiredClusterRoleBinding)
	}

	if err := ApplyMetaOptions(foundClusterRoleBinding, metaOptions...); err != nil {
		return nil, err
	}
	foundClusterRoleBinding.Subjects = subjects
	foundClusterRoleBinding.RoleRef = roleRef

	return foundClusterRoleBinding, cli.Update(ctx, foundClusterRoleBinding)
}

// DeleteClusterRoleBinding simply calls delete on a ClusterRoleBinding with the given name. Any error is returned. Check for IsNotFound.
func DeleteClusterRoleBinding(ctx context.Context, cli client.Client, name string) error {
	desiredClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	return cli.Delete(ctx, desiredClusterRoleBinding)
}
