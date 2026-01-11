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

	userv1 "github.com/openshift/api/user/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

	return nil
}

func bindRole(ctx context.Context, rr *odhtypes.ReconciliationRequest, groups []string, roleBindingName string, roleName string) error {
	// Fetch application namespace from DSCI.
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return err
	}

	groupsToBind := []rbacv1.Subject{}
	for _, e := range groups {
		// we want to disallow adding system:authenticated to the adminGroups
		if roleName == "admingroup-role" && e == "system:authenticated" || e == "" {
			log := logf.FromContext(ctx)
			log.Info("skipping adding invalid group to RoleBinding")
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
			Namespace: appNamespace,
		},
		Subjects: groupsToBind,
		RoleRef: rbacv1.RoleRef{
			APIGroup: gvk.Role.Group,
			Kind:     gvk.Role.Kind,
			Name:     roleName,
		},
	}
	err = rr.AddResources(rb)
	if err != nil {
		return errors.New("error creating RoleBinding for group")
	}

	return nil
}

func bindClusterRole(ctx context.Context, rr *odhtypes.ReconciliationRequest, groups []string, roleBindingName string, roleName string) error {
	groupsToBind := []rbacv1.Subject{}
	for _, e := range groups {
		// we want to disallow adding system:authenticated to the adminGroups
		if roleName == "admingroupcluster-role" && e == "system:authenticated" || e == "" {
			log := logf.FromContext(ctx)
			log.Info("skipping adding invalid group to ClusterRoleBinding")
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

func managePermissions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ai, ok := rr.Instance.(*serviceApi.Auth)
	if !ok {
		return errors.New("instance is not of type *services.Auth")
	}

	err := bindRole(ctx, rr, ai.Spec.AdminGroups, "admingroup-rolebinding", "admingroup-role")
	if err != nil {
		return err
	}

	err = bindClusterRole(ctx, rr, ai.Spec.AdminGroups, "admingroupcluster-rolebinding", "admingroupcluster-role")
	if err != nil {
		return err
	}

	err = bindClusterRole(ctx, rr, ai.Spec.AllowedGroups, "allowedgroupcluster-rolebinding", "allowedgroupcluster-role")
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
