package e2e_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

type AuthControllerTestCtx struct {
	*testContext
	testAuthInstance serviceApi.Auth
}

func authControllerTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext()
	require.NoError(t, err)

	authServiceCtx := AuthControllerTestCtx{
		testContext: tc,
	}

	t.Run(tc.testDsc.Name, func(t *testing.T) {
		t.Run("Auto creation of Auth CR", func(t *testing.T) {
			err = authServiceCtx.validateAuthCRCreation()
			require.NoError(t, err, "error getting Auth CR")
		})
		t.Run("Test Auth CR content", func(t *testing.T) {
			err = authServiceCtx.validateAuthCRDefaultContent()
			require.NoError(t, err, "unexpected content in Auth CR")
		})
		t.Run("Test role creation", func(t *testing.T) {
			err = authServiceCtx.validateAuthCRRoleCreation()
			require.NoError(t, err, "error getting created roles")
		})
		t.Run("Test rolebinding creation", func(t *testing.T) {
			err = authServiceCtx.validateAuthCRRoleBindingCreation()
			require.NoError(t, err, "error getting created rolebindings")
		})
		t.Run("Test rolebinding is added when group is added", func(t *testing.T) {
			err = authServiceCtx.validateAddingGroups()
			require.NoError(t, err, "error getting created rolebindings")
		})
		t.Run("Test clusterrole is added when group is added", func(t *testing.T) {
			err = authServiceCtx.validateAuthCRClusterRoleCreation()
			require.NoError(t, err, "error getting created rolebindings")
		})
		t.Run("Test clusterrolebinding is added when group is added", func(t *testing.T) {
			err = authServiceCtx.validateAuthCRClusterRoleBindingCreation()
			require.NoError(t, err, "error getting created rolebindings")
		})
		t.Run("Test bindings are removed when a group is removed", func(t *testing.T) {
			err = authServiceCtx.validateRemovingGroups()
			require.NoError(t, err, "error validating group removal")
		})
	})
}

func (tc *AuthControllerTestCtx) WithT(t *testing.T) *WithT {
	t.Helper()

	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(1 * time.Second)

	return g
}

func (tc *AuthControllerTestCtx) validateAuthCRCreation() error {
	authList := &serviceApi.AuthList{}
	if err := tc.testContext.customClient.List(tc.ctx, authList); err != nil {
		return fmt.Errorf("unable to find Auth CR instance: %w", err)
	}

	switch {
	case len(authList.Items) == 1:
		tc.testAuthInstance = authList.Items[0]
		return nil
	case len(authList.Items) > 1:
		return fmt.Errorf("only one Auth CR expected, found %v", len(authList.Items))
	default:
		return nil
	}
}

func (tc *AuthControllerTestCtx) validateAuthCRDefaultContent() error {
	if len(tc.testAuthInstance.Spec.AdminGroups) == 0 {
		return errors.New("AdminGroups is empty ")
	}

	switch tc.platform {
	case cluster.SelfManagedRhoai:
		if tc.testAuthInstance.Spec.AdminGroups[0] == "rhods-admins" {
			return nil
		}
		return fmt.Errorf("expected rhods-admins, found %v", tc.testAuthInstance.Spec.AdminGroups[0])
	case cluster.ManagedRhoai:
		if tc.testAuthInstance.Spec.AdminGroups[0] == "dedicated-admins" {
			return nil
		}
		return fmt.Errorf("expected dedicated-admins, found %v", tc.testAuthInstance.Spec.AdminGroups[0])
	case cluster.OpenDataHub:
		if tc.testAuthInstance.Spec.AdminGroups[0] != "odh-admins" {
			return fmt.Errorf("expected odh-admins, found %v", tc.testAuthInstance.Spec.AdminGroups[0])
		}
	}

	if tc.testAuthInstance.Spec.AllowedGroups[0] != "system:authenticated" {
		return fmt.Errorf("expected system:authenticated, found %v", tc.testAuthInstance.Spec.AllowedGroups[0])
	}

	return nil
}

func (tc *AuthControllerTestCtx) validateAuthCRRoleCreation() error {
	adminRole := &rbacv1.Role{}
	allowedRole := &rbacv1.Role{}

	fmt.Print("this is the ns " + tc.testContext.applicationsNamespace)
	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.testContext.applicationsNamespace, Name: "admingroup-role"}, adminRole); err != nil {
		return err
	}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.testContext.applicationsNamespace, Name: "allowedgroup-role"}, allowedRole); err != nil {
		return err
	}

	return nil
}

func (tc *AuthControllerTestCtx) validateAuthCRClusterRoleCreation() error {
	adminClusterRole := &rbacv1.ClusterRole{}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Name: "admingroupcluster-role"}, adminClusterRole); err != nil {
		return err
	}

	return nil
}

func (tc *AuthControllerTestCtx) validateAuthCRRoleBindingCreation() error {
	adminRolebinding := &rbacv1.RoleBinding{}
	allowedRolebinding := &rbacv1.RoleBinding{}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.testContext.applicationsNamespace,
		Name: "admingroup-rolebinding"}, adminRolebinding); err != nil {
		return err
	}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.applicationsNamespace, Name: "allowedgroup-rolebinding"}, allowedRolebinding); err != nil {
		return err
	}

	return nil
}

func (tc *AuthControllerTestCtx) validateAuthCRClusterRoleBindingCreation() error {
	adminClusterRolebinding := &rbacv1.ClusterRoleBinding{}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.applicationsNamespace,
		Name: "admingroupcluster-rolebinding"}, adminClusterRolebinding); err != nil {
		return err
	}

	return nil
}

func (tc *AuthControllerTestCtx) validateAddingGroups() error {
	tc.testAuthInstance.Spec.AdminGroups = append(tc.testAuthInstance.Spec.AdminGroups, "aTestAdminGroup")
	tc.testAuthInstance.Spec.AllowedGroups = append(tc.testAuthInstance.Spec.AllowedGroups, "aTestAllowedGroup")
	err := tc.customClient.Update(tc.ctx, &tc.testAuthInstance)
	if err != nil {
		fmt.Println("Error updating groups in Auth CR: ", err)
	}

	adminRolebinding := &rbacv1.RoleBinding{}
	adminClusterRolebinding := &rbacv1.ClusterRoleBinding{}
	allowedRolebinding := &rbacv1.RoleBinding{}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.applicationsNamespace, Name: "admingroup-rolebinding"}, adminRolebinding); err != nil {
		if adminRolebinding.Subjects[1].Name != "aTestAdminGroup" {
			return fmt.Errorf("Expected aTestAdminGroup found %s ", adminRolebinding.Subjects[1].Name)
		}
	}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.applicationsNamespace,
		Name: "admingroupcluster-rolebinding"}, adminClusterRolebinding); err != nil {
		if adminRolebinding.Subjects[1].Name != "aTestAdminGroup" {
			return fmt.Errorf("Expected aTestAdminGroup found %s ", adminRolebinding.Subjects[1].Name)
		}
	}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.applicationsNamespace, Name: "allowedgroup-rolebinding"}, allowedRolebinding); err != nil {
		if allowedRolebinding.Subjects[1].Name != "aTestAllowedGroup" {
			return fmt.Errorf("Expected aTestAllowedGroup found %s ", allowedRolebinding.Subjects[1].Name)
		}
	}

	return nil
}

func (tc *AuthControllerTestCtx) validateRemovingGroups() error {
	expectedGroup := "odh-admins"
	if tc.platform == cluster.ManagedRhoai || tc.platform == cluster.SelfManagedRhoai {
		expectedGroup = "rhods-admins"
	}
	if _, err := controllerutil.CreateOrUpdate(tc.ctx, tc.customClient, &tc.testAuthInstance, func() error {
		tc.testAuthInstance.Spec.AdminGroups = []string{expectedGroup}
		return nil
	}); err != nil {
		return errors.New("error removing groups from auth CR")
	}

	adminRolebinding := &rbacv1.RoleBinding{}
	adminClusterRolebinding := &rbacv1.ClusterRoleBinding{}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.applicationsNamespace, Name: "admingroup-rolebinding"}, adminRolebinding); err != nil {
		if len(adminRolebinding.Subjects) > 1 {
			return fmt.Errorf("Expected 1 subject in adminRoleBinding found %v", len(adminRolebinding.Subjects))
		}
		if adminRolebinding.Subjects[0].Name != expectedGroup {
			return fmt.Errorf("Expected adminRolebinding to only contain %s found %s", expectedGroup, adminRolebinding.Subjects[0].Name)
		}
	}

	if err := tc.testContext.customClient.Get(tc.ctx, types.NamespacedName{Namespace: tc.applicationsNamespace,
		Name: "admingroupcluster-rolebinding"}, adminClusterRolebinding); err != nil {
		if len(adminClusterRolebinding.Subjects) > 1 {
			return fmt.Errorf("Expected 1 subject in adminClusterRoleBinding found %v", len(adminClusterRolebinding.Subjects))
		}
		if adminClusterRolebinding.Subjects[0].Name != expectedGroup {
			return fmt.Errorf("Expected adminClusterRolebinding to only contain %s found %s", expectedGroup, adminClusterRolebinding.Subjects[0].Name)
		}
	}

	return nil
}
