package dscinitialization

import (
	"context"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/dashboard"
)

func (r *DSCInitializationReconciler) createAuth(ctx context.Context) error {
	// Create Auth CR singleton
	defaultAuth := client.Object(&serviceApi.Auth{
		TypeMeta: metav1.TypeMeta{
			Kind:       serviceApi.AuthKind,
			APIVersion: serviceApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.AuthInstanceName,
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{dashboard.GetAdminGroup()},
			AllowedGroups: []string{"system:authenticated"},
		},
	},
	)
	err := r.Create(ctx, defaultAuth)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}

	return nil
}
