package obo

import (
	"context"
	"embed"
	"path"

	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	obo "github.com/opendatahub-io/opendatahub-operator/v2/pkg/observability"
)

func createViewerRoleBinding(f *feature.Feature) error {
	desiredRoleBinding := &authv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rhoai-monitoring-stack-view",
			Namespace: f.Spec.MonNamespace,
		},
		Subjects: []authv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: f.Spec.MonNamespace,
				Name:      "rhoai-monitoring-stack-prometheus",
			},
		},
		RoleRef: authv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-monitoring-view",
		},
	}
	foundRoleBinding := &authv1.RoleBinding{}
	err := f.Client.Get(context.TODO(), types.NamespacedName{
		Name:      "rhoai-monitoring-stack-view",
		Namespace: f.Spec.MonNamespace,
	}, foundRoleBinding)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// Do we need to set Controller reference?
			err = f.Client.Create(context.TODO(), desiredRoleBinding)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func ConfigureFederation(sourceDir embed.FS, dsciSpec *dsciv1.DSCInitializationSpec) feature.Action {
	return func(f *feature.Feature) error {
		if err := cluster.CreateSecret(f.Client, "prometheus-secret", f.Spec.MonNamespace); err != nil {
			return err
		}
		if err := createViewerRoleBinding(f); err != nil {
			return err
		}

		return obo.CreatePrometheusConfigs(
			context.TODO(),
			f.Client,
			f.Enabled,
			sourceDir,
			path.Join("resources", "observability", "federate"),
			dsciSpec)
	}
}

func ConfigureOperatorMetircs(sourceDir embed.FS, dsciSpec *dsciv1.DSCInitializationSpec) feature.Action {
	return func(f *feature.Feature) error {
		return obo.CreatePrometheusConfigs(
			context.TODO(),
			f.Client,
			f.Enabled,
			sourceDir,
			path.Join("resources", "observability", "rhoai-metrics"),
			dsciSpec)
	}
}
