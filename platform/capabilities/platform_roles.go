package capabilities

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/odh-platform/pkg/platform"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// CreateOrUpdatePlatformRBAC ensures that platform controllers have right RBAC for the given object references.
func CreateOrUpdatePlatformRBAC(ctx context.Context, cli client.Client, roleName string,
	objectReferences []platform.ResourceReference, metaOptions ...cluster.MetaOptions) error {
	if _, err := cluster.CreateOrUpdateClusterRole(ctx, cli, roleName, createPolicyRules(objectReferences), metaOptions...); err != nil {
		return fmt.Errorf("failed creating cluster role: %w", err)
	}

	// TODO: this assumes the platform controllers are embedded in the operator and it's the operator ServiceAccount that require the roles
	namespace, errNS := cluster.GetOperatorNamespace()
	if errNS != nil {
		return fmt.Errorf("failed getting operator namespace: %w", errNS)
	}

	subjects, roleRef := createPlatformRoleBinding(roleName, namespace)
	if _, err := cluster.CreateOrUpdateClusterRoleBinding(ctx, cli, roleName, subjects, roleRef, metaOptions...); err != nil {
		return fmt.Errorf("failed creating cluster role binding: %w", err)
	}

	return nil
}

func createPolicyRules(objectReferences []platform.ResourceReference) []rbacv1.PolicyRule {
	apiGroups := make([]string, 0)
	resources := make([]string, 0)
	for _, ref := range objectReferences {
		apiGroups = append(apiGroups, ref.GroupVersionKind.Group)
		resources = append(resources, ref.Resources)
	}

	return []rbacv1.PolicyRule{
		{
			APIGroups: apiGroups,
			Resources: resources,
			Verbs:     []string{"get", "list", "watch", "update", "patch"},
		},
	}
}

func createPlatformRoleBinding(roleName, namespace string) ([]rbacv1.Subject, rbacv1.RoleRef) {
	return []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      "opendatahub-operator-controller-manager",
				Namespace: namespace,
			},
		},
		rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     roleName,
		}
}
