package dscinitialization

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func CreateAuth(ctx context.Context, cli client.Client, dscInit *dsciv1.DSCInitialization) error {
	// check if a dashboardConfig instance exists.
	odhObject := resources.GvkToUnstructured(gvk.OdhDashboardConfig)

	err := cli.Get(ctx, client.ObjectKey{
		Name:      "odh-dashboard-config",
		Namespace: dscInit.Spec.ApplicationsNamespace,
	}, odhObject)

	auth := serviceApi.Auth{}
	auth.Name = serviceApi.AuthInstanceName

	switch {
	case meta.IsNoMatchError(err) || k8serr.IsNotFound(err):
		// if a dashboardConfig type does not exist or the instance is not found.
		auth.Spec.AdminGroups = []string{dashboard.GetAdminGroup()}
		auth.Spec.AllowedGroups = []string{"system:authenticated"}
	case err != nil:
		return fmt.Errorf("failed to get odh-dashboard-config instance: %w", err)
	default:
		// dashboardConfig CRD exists, and we have an instance so copy the groups to the auth CR
		foundGroups, ok, err := unstructured.NestedStringMap(odhObject.Object, "spec", "groupsConfig")
		if err != nil {
			return err
		}

		if ok {
			auth.Spec.AdminGroups = common.SplitUnique(foundGroups["adminGroups"], ",")
			auth.Spec.AllowedGroups = common.SplitUnique(foundGroups["allowedGroups"], ",")
		}
	}

	err = cli.Create(ctx, &auth)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}

	return nil
}
