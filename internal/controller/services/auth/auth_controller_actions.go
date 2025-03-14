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

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Templates = []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: AdminGroupRoleTemplate,
		},
		{
			FS:   resourcesFS,
			Path: AllowedGroupRoleTemplate,
		},
		{
			FS:   resourcesFS,
			Path: AdminGroupClusterRoleTemplate,
		},
	}

	return nil
}

func bindRole(ctx context.Context, rr *odhtypes.ReconciliationRequest, groups []string, roleBindingName string, roleName string) error {
	groupsToBind := []rbacv1.Subject{}
	for _, e := range groups {
		// we want to disallow adding system:authenticated to the adminGroups
		if roleName == "admingroup-role" && e == "system:authenticated" || e == "" {
			log := logf.FromContext(ctx)
			log.Info("skipping adding invalid group to RoleBinding")
			continue
		}
		rs := rbacv1.Subject{
			Kind:     "Group",
			APIGroup: "rbac.authorization.k8s.io",
			Name:     e,
		}
		groupsToBind = append(groupsToBind, rs)
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: rr.DSCI.Spec.ApplicationsNamespace,
		},
		Subjects: groupsToBind,
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
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
	groupsToBind := []rbacv1.Subject{}
	for _, e := range groups {
		// we want to disallow adding system:authenticated to the adminGroups
		if roleName == "admingroupcluster-role" && e == "system:authenticated" || e == "" {
			log := logf.FromContext(ctx)
			log.Info("skipping adding invalid group to ClusterRoleBinding")
			continue
		}
		rs := rbacv1.Subject{
			Kind:     "Group",
			APIGroup: "rbac.authorization.k8s.io",
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
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     roleName,
		},
	}
	err := rr.AddResources(crb)
	if err != nil {
		return errors.New("error creating RoleBinding for group")
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

	err = bindRole(ctx, rr, ai.Spec.AllowedGroups, "allowedgroup-rolebinding", "allowedgroup-role")
	if err != nil {
		return err
	}

	return nil
}
