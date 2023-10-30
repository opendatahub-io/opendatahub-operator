package e2e

import (
	"context"
	"fmt"
	"log"
	"testing"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func deletionTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Deletion: DataScienceCluster instance", func(t *testing.T) {
			err = testCtx.testDSCDeletion()
			require.NoError(t, err, "Error to delete DSC instance")
		})
		t.Run("Deletion: Application Resource", func(t *testing.T) {
			err = testCtx.testAllApplicationDeletion()
			require.NoError(t, err, "Error to delete component")
		})
	})
}

func (tc *testContext) testDSCDeletion() error {
	// Delete test DataScienceCluster resource if found

	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	expectedDSC := &dsc.DataScienceCluster{}

	err := tc.customClient.Get(tc.ctx, dscLookupKey, expectedDSC)
	if err == nil {
		dscerr := tc.customClient.Delete(tc.ctx, expectedDSC, &client.DeleteOptions{})
		if dscerr != nil {
			return fmt.Errorf("error deleting DSC instance %s: %v", expectedDSC.Name, dscerr)
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

	if err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (done bool, err error) {
		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.opendatahub.io/" + component.GetComponentName(),
		})
		if err != nil {
			log.Printf("error listing component deployments :%v. Trying again...", err)
			return false, err
		}
		if len(appList.Items) != 0 {
			return false, nil
		} else {
			return true, nil
		}
	}); err != nil {
		return fmt.Errorf("error deleting component: %v", component.GetComponentName())
	}

	return nil
}

func (tc *testContext) testAllApplicationDeletion() error {
	// Deletion all listed components' deployments

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.Dashboard)); err != nil {
		return err
	}

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.ModelMeshServing)); err != nil {
		return err
	}

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.Kserve)); err != nil {
		return err
	}

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.Workbenches)); err != nil {
		return err
	}

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.DataSciencePipelines)); err != nil {
		return err
	}

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.CodeFlare)); err != nil {
		return err
	}

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.Ray)); err != nil {
		return err
	}

	if err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.TrustyAI)); err != nil {
		return err
	}

	return nil
}
