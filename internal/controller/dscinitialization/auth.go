package dscinitialization

import (
	"context"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

var (
	adminGroups = map[common.Platform]string{
		cluster.SelfManagedRhoai: "rhods-admins",
		cluster.ManagedRhoai:     "dedicated-admins",
		cluster.OpenDataHub:      "odh-admins",
	}
)

func (r *DSCInitializationReconciler) createAuth(ctx context.Context, platform common.Platform) error {
	a := serviceApi.Auth{}
	// Auth CR exists, we do nothing
	err := r.Client.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, &a)
	if err == nil {
		return nil
	}

	if !k8serr.IsNotFound(err) {
		return err
	}
	// Auth CR not found, create default Auth CR
	if err := r.Client.Create(ctx, buildDefaultAuth(platform)); err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func buildDefaultAuth(platform common.Platform) client.Object {
	// Get admin group for the platform, with fallback to OpenDataHub admin group
	adminGroup := adminGroups[platform]
	if adminGroup == "" {
		adminGroup = adminGroups[cluster.OpenDataHub]
	}

	return &serviceApi.Auth{
		TypeMeta:   metav1.TypeMeta{Kind: serviceApi.AuthKind, APIVersion: serviceApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{adminGroup},
			AllowedGroups: []string{"system:authenticated"},
		},
	}
}
