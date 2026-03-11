package azure

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

func NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &ccmv1alpha1.AzureKubernetesEngine{}).
		// TODO: use dynamic owned resources once merged: https://github.com/opendatahub-io/opendatahub-operator/pull/3188
		Owns(&corev1.Secret{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&appsv1.Deployment{}).
		WithAction(initialize).
		ComposeWith(certmanager.Bootstrap[*ccmv1alpha1.AzureKubernetesEngine](
			ccmv1alpha1.AzureKubernetesEngineInstanceName, certmanager.DefaultBootstrapConfig())).
		WithAction(cloudmanager.NewReconcileAction()).
		Build(ctx)
	if err != nil {
		return err
	}

	return nil
}
