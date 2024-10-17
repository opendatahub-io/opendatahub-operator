package e2e_test

import (
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func deletionTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	// pre-check before deletion
	t.Run("Ensure all components created", func(t *testing.T) {
		err = testCtx.testAllComponentCreation(t)
		require.NoError(t, err, "Not all components are created")
	})

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Deletion DSC instance", func(t *testing.T) {
			err = testCtx.testDeletionExistDSC()
			require.NoError(t, err, "Error to delete DSC instance")
		})
		t.Run("Check all component resource are deleted", func(t *testing.T) {
			err = testCtx.testAllApplicationDeletion(t)
			require.NoError(t, err, "Should not found component exist")
		})
		t.Run("Deletion DSCI instance", func(t *testing.T) {
			err = testCtx.testDeletionExistDSCI()
			require.NoError(t, err, "Error to delete DSCI instance")
		})
	})
}

func (tc *testContext) testDeletionExistDSC() error {
	// Delete test DataScienceCluster resource if found

	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	expectedDSC := &dscv1.DataScienceCluster{}

	err := tc.customClient.Get(tc.ctx, dscLookupKey, expectedDSC)
	if err == nil {
		dscerr := tc.customClient.Delete(tc.ctx, expectedDSC, &client.DeleteOptions{})
		if dscerr != nil {
			return fmt.Errorf("error deleting DSC instance %s: %w", expectedDSC.Name, dscerr)
		}
	} else if !errors.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("could not find DSC instance to delete: %w", err)
		}
	}
	return nil
}

func (tc *testContext) testComponentDeletion(component components.ComponentInterface) error {
	// Deletion of Deployments
	if err := wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, componentDeletionTimeout, true, func(ctx context.Context) (bool, error) {
		var componentName = component.GetComponentName()
		if component.GetComponentName() == "dashboard" { // special case for RHOAI dashboard name
			componentName = "rhods-dashboard"
		}

		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component(componentName),
		})
		if err != nil {
			log.Printf("error getting component deployments :%v. Trying again...", err)

			return false, err
		}

		return len(appList.Items) == 0, nil
	}); err != nil {
		return fmt.Errorf("error to find component still exist: %v", component.GetComponentName())
	}

	return nil
}

func (tc *testContext) testAllApplicationDeletion(t *testing.T) error { //nolint:thelper
	// Deletion all listed components' deployments

	components, err := tc.testDsc.GetComponents()
	if err != nil {
		return err
	}

	for _, c := range components {
		c := c
		t.Run("Delete "+c.GetComponentName(), func(t *testing.T) {
			t.Parallel()
			err = tc.testComponentDeletion(c)
			require.NoError(t, err)
		})
	}

	return nil
}

// To test if  DSCI CR is in the cluster and no problem to delete it
// if fail on any of these two conditios, fail test.
func (tc *testContext) testDeletionExistDSCI() error {
	// Delete test DSCI resource if found

	dsciLookupKey := types.NamespacedName{Name: tc.testDSCI.Name}
	expectedDSCI := &dsciv1.DSCInitialization{}

	err := tc.customClient.Get(tc.ctx, dsciLookupKey, expectedDSCI)
	if err == nil {
		dscierr := tc.customClient.Delete(tc.ctx, expectedDSCI, &client.DeleteOptions{})
		if dscierr != nil {
			return fmt.Errorf("error deleting DSCI instance %s: %w", expectedDSCI.Name, dscierr)
		}
	} else if !errors.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("could not find DSCI instance to delete :%w", err)
		}
	}
	return nil
}
