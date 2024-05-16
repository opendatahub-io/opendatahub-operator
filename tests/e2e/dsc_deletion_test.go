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

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
)

func deletionTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	t.Run("ensure all components created", func(t *testing.T) {
		err = testCtx.testAllApplicationCreation(t)
		require.NoError(t, err, "Error to create DSC instance")
	})

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Deletion: DataScienceCluster instance", func(t *testing.T) {
			err = testCtx.testDeletionExistDSC()
			require.NoError(t, err, "Error to delete DSC instance")
		})
		t.Run("Deletion: Application Resource", func(t *testing.T) {
			err = testCtx.testAllApplicationDeletion()
			require.NoError(t, err, "Error to delete component")
		})
		t.Run("Deletion: DSCI instance", func(t *testing.T) {
			err = testCtx.testDeletionExistDSCI()
			require.NoError(t, err, "Error to delete DSCI instance")
		})
	})
}

func (tc *testContext) testDeletionExistDSC() error {
	// Delete test DataScienceCluster resource if found

	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	expectedDSC := &dsc.DataScienceCluster{}

	err := tc.customClient.Get(tc.ctx, dscLookupKey, expectedDSC)
	if err == nil {
		dscerr := tc.customClient.Delete(tc.ctx, expectedDSC, &client.DeleteOptions{})
		if dscerr != nil {
			return fmt.Errorf("error deleting DSC instance %s: %w", expectedDSC.Name, dscerr)
		}
	} else if !errors.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("error getting DSC instance :%w", err)
		}
	}

	return nil
}

func (tc *testContext) testApplicationDeletion(component components.ComponentInterface) error {
	// Deletion of Deployments

	if err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (bool, error) {
		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: odhLabelPrefix + component.GetComponentName(),
		})
		if err != nil {
			log.Printf("error listing component deployments :%v. Trying again...", err)

			return false, err
		}

		return len(appList.Items) == 0, nil
	}); err != nil {
		return fmt.Errorf("error deleting component: %v", component.GetComponentName())
	}

	return nil
}

func (tc *testContext) testAllApplicationDeletion() error {
	// Deletion all listed components' deployments

	components, err := tc.testDsc.GetComponents()
	if err != nil {
		return err
	}

	for _, c := range components {
		if err = tc.testApplicationDeletion(c); err != nil {
			return err
		}
	}

	return nil
}

// To test if  DSCI CR is in the cluster and no problem to delete it
// if fail on any of these two conditios, fail test.
func (tc *testContext) testDeletionExistDSCI() error {
	// Delete test DSCI resource if found

	dsciLookupKey := types.NamespacedName{Name: tc.testDSCI.Name}
	expectedDSCI := &dsci.DSCInitialization{}

	err := tc.customClient.Get(tc.ctx, dsciLookupKey, expectedDSCI)
	if err == nil {
		dscierr := tc.customClient.Delete(tc.ctx, expectedDSCI, &client.DeleteOptions{})
		if dscierr != nil {
			return fmt.Errorf("error deleting DSCI instance %s: %w", expectedDSCI.Name, dscierr)
		}
	} else if !errors.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("error getting DSCI instance :%w", err)
		}
	}

	return nil
}
