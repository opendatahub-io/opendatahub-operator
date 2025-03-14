package dscinitialization

import (
	"context"
	"fmt"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
)

func (r *DSCInitializationReconciler) createAuth(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)
	// check for the dashboardConfig kind.
	// Once the groupsConfig entry in the dashboardConfig is removed this logic can be removed.
	crd := &apiextv1.CustomResourceDefinition{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: "odhdashboardconfigs.opendatahub.io"}, crd)

	// can't find the dashboardConfig kind so create the default.
	if err != nil && k8serr.IsNotFound(err) {
		log.Info("couldn't find odhDashboardConfig CRD, creating auth with defaults.")
		err = r.Create(ctx, buildDefaultAuth())
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return err
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get odhdashboardconfigs.opendatahub.io CRD: %w", err)
	}

	// check if a dashboardConfig instance exists.
	odhObject := &unstructured.Unstructured{}
	odhObject.SetGroupVersionKind(gvk.OdhDashboardConfig)
	err = r.Client.Get(ctx, client.ObjectKey{
		Name:      "odh-dashboard-config",
		Namespace: dscInit.Spec.ApplicationsNamespace,
	}, odhObject)
	if err != nil && k8serr.IsNotFound(err) {
		log.Info("couldn't find odhDashboardConfig instance, creating auth with defaults.")
		err = r.Create(ctx, buildDefaultAuth())
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return err
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get odh-dashboard-config instance: %w", err)
	}

	// dashboardConfig CRD exists and we have an instance so copy the groups to the auth CR
	foundGroups, ok, err := unstructured.NestedStringMap(odhObject.Object, "spec", "groupsConfig")
	if err != nil {
		return err
	}
	if ok {
		adminGroup := []string{}
		allowedGroup := []string{}
		added := common.AddMissing(&adminGroup, foundGroups["adminGroups"])
		added += common.AddMissing(&allowedGroup, foundGroups["allowedGroups"])

		// only update if we found a new group in the list
		if added == 0 {
			return nil
		}
		err = r.Create(ctx, buildAuthWithGroups(adminGroup, allowedGroup))
		if err != nil && !k8serr.IsAlreadyExists(err) {
			return err
		}
		return nil
	}

	// found a dashboardConfig CRD and instance but no groupsConfig so instantiate an empty auth.
	err = r.Create(ctx, buildEmptyAuth())
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func buildAuthWithGroups(adminGroup []string, allowedGroup []string) client.Object {
	return &serviceApi.Auth{
		TypeMeta:   metav1.TypeMeta{Kind: serviceApi.AuthKind, APIVersion: serviceApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
		Spec:       serviceApi.AuthSpec{AdminGroups: adminGroup, AllowedGroups: allowedGroup},
	}
}

func buildDefaultAuth() client.Object {
	return &serviceApi.Auth{
		TypeMeta:   metav1.TypeMeta{Kind: serviceApi.AuthKind, APIVersion: serviceApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
		Spec:       serviceApi.AuthSpec{AdminGroups: []string{dashboard.GetAdminGroup()}, AllowedGroups: []string{"system:authenticated"}},
	}
}

func buildEmptyAuth() client.Object {
	return &serviceApi.Auth{
		TypeMeta:   metav1.TypeMeta{Kind: serviceApi.AuthKind, APIVersion: serviceApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
		Spec:       serviceApi.AuthSpec{AdminGroups: []string{}, AllowedGroups: []string{}},
	}
}
