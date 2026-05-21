/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	userv1 "github.com/openshift/api/user/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	// systemAuthenticated represents the deprecated system:authenticated group.
	systemAuthenticated = "system:authenticated"
	// deprecatedGroupUsageCondition represents the condition type for deprecated group usage.
	deprecatedGroupUsageCondition = "DeprecatedGroupUsage"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Templates = []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: AdminGroupRoleTemplate,
		},
		{
			FS:   resourcesFS,
			Path: AdminGroupClusterRoleTemplate,
		},
		{
			FS:   resourcesFS,
			Path: AllowedGroupClusterRoleTemplate,
		},
	}

	exists, err := cluster.NamespaceExists(ctx, rr.Client, "models-as-a-service")
	if err != nil {
		return fmt.Errorf("failed to check if models-as-a-service namespace exists: %w", err)
	}
	if exists {
		rr.Templates = append(rr.Templates, odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: AdminGroupMaaSRoleTemplate,
		})
	}

	exists, err = cluster.NamespaceExists(ctx, rr.Client, "kuadrant-system")
	if err != nil {
		return fmt.Errorf("failed to check if kuadrant-system namespace exists: %w", err)
	}
	if exists {
		rr.Templates = append(rr.Templates, odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: AdminGroupKuadrantRoleTemplate,
		})
	}

	return nil
}

func bindRole(ctx context.Context, rr *odhtypes.ReconciliationRequest, groups []string, roleBindingName string, roleName string, namespace string) error {
	// Perform security validation on groups
	strictMode := isStrictModeEnabled()
	if err := validateGroupsSecurity(ctx, groups, roleName, strictMode); err != nil {
		return fmt.Errorf("role binding security validation failed for %s: %w", roleBindingName, err)
	}

	if namespace == "" {
		appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
		if err != nil {
			return err
		}
		namespace = appNamespace
	} else {
		exists, err := cluster.NamespaceExists(ctx, rr.Client, namespace)
		if err != nil {
			return err
		}
		if !exists {
			logf.FromContext(ctx).Info("namespace not found, skipping RoleBinding creation", "namespace", namespace, "roleBinding", roleBindingName)
			return nil
		}
	}

	groupsToBind := []rbacv1.Subject{}
	for _, e := range groups {
		// Enhanced security filtering: skip empty groups and system:authenticated for all roles
		// Note: API validation should prevent system:authenticated, but this provides defense in depth
		if e == "" || e == systemAuthenticated {
			log := logf.FromContext(ctx)
			log.Info("skipping adding invalid group to RoleBinding", "group", e, "role", roleName, "reason", "empty or security-restricted group")
			continue
		}

		rs := rbacv1.Subject{
			Kind:     gvk.Group.Kind,
			APIGroup: gvk.Group.Group,
			Name:     e,
		}
		groupsToBind = append(groupsToBind, rs)
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: namespace,
		},
		Subjects: groupsToBind,
		RoleRef: rbacv1.RoleRef{
			APIGroup: gvk.Role.Group,
			Kind:     gvk.Role.Kind,
			Name:     roleName,
		},
	}
	err := rr.AddResources(rb)
	if err != nil {
		return errors.New("error creating RoleBinding for group")
	}

	return nil
}

func bindClusterRole(ctx context.Context, rr *odhtypes.ReconciliationRequest, groups []string, roleBindingName string, roleName string) error {
	// Perform security validation on groups
	strictMode := isStrictModeEnabled()
	if err := validateGroupsSecurity(ctx, groups, roleName, strictMode); err != nil {
		return fmt.Errorf("cluster role binding security validation failed for %s: %w", roleBindingName, err)
	}

	groupsToBind := []rbacv1.Subject{}
	for _, e := range groups {
		// Enhanced security filtering: skip empty groups and system:authenticated for all roles
		// Note: API validation should prevent system:authenticated, but this provides defense in depth
		if e == "" || e == systemAuthenticated {
			log := logf.FromContext(ctx)
			log.Info("skipping adding invalid group to ClusterRoleBinding", "group", e, "role", roleName, "reason", "empty or security-restricted group")
			continue
		}

		rs := rbacv1.Subject{
			Kind:     gvk.Group.Kind,
			APIGroup: gvk.Group.Group,
			Name:     e,
		}

		groupsToBind = append(groupsToBind, rs)
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: roleBindingName,
		},
		Subjects: groupsToBind,
		RoleRef: rbacv1.RoleRef{
			Kind:     gvk.ClusterRole.Kind,
			APIGroup: gvk.ClusterRole.Group,
			Name:     roleName,
		},
	}
	err := rr.AddResources(crb)
	if err != nil {
		return errors.New("error creating ClusterRoleBinding for group")
	}

	return nil
}

// validateGroupsSecurity performs security validation of groups and can reject system:authenticated
// based on configuration. This provides a migration path for existing deployments.
func validateGroupsSecurity(ctx context.Context, groups []string, roleName string, strictMode bool) error {
	log := logf.FromContext(ctx)

	for _, group := range groups {
		if group == systemAuthenticated {
			if strictMode {
				return fmt.Errorf("security violation: group 'system:authenticated' is not allowed for role '%s'. "+
					"This violates Kubernetes security best practices. "+
					"Please use explicit groups like 'odh-users', 'rhods-users', or namespace-specific groups. "+
					"See https://kubernetes.io/docs/concepts/security/rbac-good-practices/ for guidance",
					roleName)
			} else {
				// Warn but allow for backward compatibility during migration period
				log.Info("SECURITY WARNING: system:authenticated detected in role binding - this will be rejected in strict mode",
					"role", roleName,
					"group", group,
					"recommendation", "Migrate to explicit groups before enabling strict security mode",
					"enableStrictMode", "Set STRICT_SECURITY_MODE=true environment variable to test rejection")
			}
		}
	}
	return nil
}

// isStrictModeEnabled checks if strict security mode is enabled via environment variable.
func isStrictModeEnabled() bool {
	strictMode := os.Getenv("STRICT_SECURITY_MODE")
	return strictMode == "true" || strictMode == "1"
}

// logDeprecationWarnings logs deprecation warnings for system:authenticated usage.
func logDeprecationWarnings(ctx context.Context, auth *serviceApi.Auth) {
	log := logf.FromContext(ctx)

	// Check AdminGroups for system:authenticated (should already be blocked by API validation, but defensive logging)
	for _, group := range auth.Spec.AdminGroups {
		if group == systemAuthenticated {
			log.Info("SECURITY WARNING: system:authenticated detected in AdminGroups - this should have been blocked by API validation",
				"authCR", auth.Name,
				"field", "adminGroups",
				"recommendation", "Use platform-specific admin groups like 'odh-admins' or 'rhods-admins'",
				"securityIssue", "system:authenticated grants admin access to any authenticated user",
				"deprecationPhase", "immediate-rejection")
		}
	}

	// Check AllowedGroups for system:authenticated
	for _, group := range auth.Spec.AllowedGroups {
		if group == systemAuthenticated {
			log.Info("SECURITY WARNING: system:authenticated detected in AllowedGroups - this violates Kubernetes security best practices",
				"authCR", auth.Name,
				"field", "allowedGroups",
				"recommendation", "Use explicit groups like 'odh-users', 'rhods-users', or namespace-specific groups",
				"securityIssue", "system:authenticated grants access to any authenticated user",
				"deprecationPhase", "migration-required",
				"migrationGuide", "https://kubernetes.io/docs/concepts/security/rbac-good-practices/")
		}
	}
}

// updateDeprecationStatusConditions updates the Auth CR status with deprecation warnings.
func updateDeprecationStatusConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest, auth *serviceApi.Auth) {
	hasDeprecatedUsage := false
	var deprecationMessages []string

	// Check for deprecated usage in AdminGroups
	if slices.Contains(auth.Spec.AdminGroups, systemAuthenticated) {
		hasDeprecatedUsage = true
		deprecationMessages = append(deprecationMessages, "AdminGroups contains deprecated 'system:authenticated' group")
	}

	// Check for deprecated usage in AllowedGroups
	if slices.Contains(auth.Spec.AllowedGroups, systemAuthenticated) {
		hasDeprecatedUsage = true
		deprecationMessages = append(deprecationMessages, "AllowedGroups contains deprecated 'system:authenticated' group")
	}

	if hasDeprecatedUsage {
		// Add deprecation warning condition
		deprecationCondition := common.Condition{
			Type:   deprecatedGroupUsageCondition,
			Status: metav1.ConditionTrue,
			Reason: "SystemAuthenticatedDeprecated",
			Message: fmt.Sprintf("Deprecated usage detected: %s. Please migrate to explicit groups for security compliance. "+
				"See https://kubernetes.io/docs/concepts/security/rbac-good-practices/", deprecationMessages[0]),
			LastTransitionTime: metav1.Now(),
		}

		// Update the status condition
		conditions := auth.GetConditions()
		conditionExists := false
		for i, cond := range conditions {
			if cond.Type == deprecatedGroupUsageCondition {
				conditions[i] = deprecationCondition
				conditionExists = true
				break
			}
		}
		if !conditionExists {
			conditions = append(conditions, deprecationCondition)
		}
		auth.SetConditions(conditions)

		// Schedule status update
		if err := rr.Client.Status().Update(ctx, auth); err != nil {
			log := logf.FromContext(ctx)
			log.Info("Failed to update Auth status with deprecation warning", "error", err)
		}
	} else {
		// Remove deprecation condition if it exists and no deprecated usage is found
		conditions := auth.GetConditions()
		for i, cond := range conditions {
			if cond.Type == deprecatedGroupUsageCondition {
				conditions = append(conditions[:i], conditions[i+1:]...)
				auth.SetConditions(conditions)
				if err := rr.Client.Status().Update(ctx, auth); err != nil {
					log := logf.FromContext(ctx)
					log.Info("Failed to remove deprecation warning from Auth status", "error", err)
				}
				break
			}
		}
	}
}

func managePermissions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ai, ok := rr.Instance.(*serviceApi.Auth)
	if !ok {
		return errors.New("instance is not of type *services.Auth")
	}

	// Log deprecation warnings for system:authenticated usage
	logDeprecationWarnings(ctx, ai)

	// Update status conditions for deprecation warnings
	updateDeprecationStatusConditions(ctx, rr, ai)

	err := bindRole(ctx, rr, ai.Spec.AdminGroups, "data-science-admingroup-rolebinding", "data-science-admingroup-role", "")
	if err != nil {
		return err
	}

	err = bindRole(ctx, rr, ai.Spec.AdminGroups, "data-science-admingroup-maas-rolebinding", "data-science-admingroup-maas-role", "models-as-a-service")
	if err != nil {
		return err
	}

	err = bindRole(ctx, rr, ai.Spec.AdminGroups, "data-science-admingroup-kuadrant-rolebinding", "data-science-admingroup-kuadrant-role", "kuadrant-system")
	if err != nil {
		return err
	}

	err = bindClusterRole(ctx, rr, ai.Spec.AdminGroups, "data-science-admingroupcluster-rolebinding", "data-science-admingroupcluster-role")
	if err != nil {
		return err
	}

	err = bindClusterRole(ctx, rr, ai.Spec.AllowedGroups, "data-science-allowedgroupcluster-rolebinding", "data-science-allowedgroupcluster-role")
	if err != nil {
		return err
	}

	return nil
}

func addUserGroup(ctx context.Context, rr *odhtypes.ReconciliationRequest, userGroupName string) error {
	namespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	userGroup := &userv1.Group{
		ObjectMeta: metav1.ObjectMeta{
			Name: userGroupName,
			// Otherwise it errors with  "error": "an empty namespace may not be set during creation"
			Namespace: namespace,
			Annotations: map[string]string{
				annotations.ManagedByODHOperator: "false",
			},
		},
		// Otherwise is errors with "error": "Group.user.openshift.io \"odh-admins\" is invalid: users: Invalid value: \"null\": users in body must be of type array: \"null\""}
		Users: []string{},
	}
	err = rr.AddResources(userGroup)
	if err != nil {
		return fmt.Errorf("unable to add user group: %w", err)
	}

	return nil
}

func createDefaultGroup(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ok, err := cluster.IsIntegratedOAuth(ctx, rr.Client)
	if err != nil {
		return err
	}
	if !ok {
		logf.Log.Info("default auth method is not enabled")
		return nil
	}

	release := cluster.GetRelease()
	switch release.Name {
	case cluster.ManagedRhoai:
		err = addUserGroup(ctx, rr, "rhods-admins")
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return err
		}
	case cluster.SelfManagedRhoai:
		err = addUserGroup(ctx, rr, "rhods-admins")
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return err
		}
	default:
		err = addUserGroup(ctx, rr, "odh-admins")
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}
