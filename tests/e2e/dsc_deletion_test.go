package e2e

import (
	"context"
	"fmt"
	"log"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"testing"

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
		t.Run("DataScienceCluster instance Deletion", func(t *testing.T) {
			err = testCtx.testDSCDeletion()
			require.NoError(t, err, "error deleting DSC object ")
		})
		t.Run("Application Resource Deletion", func(t *testing.T) {
			err = testCtx.testAllApplicationDeletion()
			require.NoError(t, err, "error testing deletion of DSC defined applications")
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
			return fmt.Errorf("error deleting test DSC %s: %v", expectedDSC.Name, dscerr)
		}
	} else if !errors.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("error getting test DSC instance :%v", err)
		}
	}
	return nil
}

func (tc *testContext) testApplicationDeletion(component components.ComponentInterface) error {

	// Deletion of Deployments
	if err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (done bool, err error) {
		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/part-of=" + component.GetComponentName(),
		})
		if err != nil {
			log.Printf("error listing application deployments :%v. Trying again...", err)
			return false, err
		}
		if len(appList.Items) != 0 {
			return false, nil
		} else {
			return true, nil
		}
	}); err != nil {
		return err
	}

	return nil
}

func (tc *testContext) testAllApplicationDeletion() error {

	err := tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.Dashboard))
	if err != nil {
		return fmt.Errorf("error deleting application %v", tc.testDsc.Spec.Components.Dashboard)
	}

	err = tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.ModelMeshServing))
	if err != nil {
		return fmt.Errorf("error deleting application %v", tc.testDsc.Spec.Components.ModelMeshServing)
	}

	err = tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.Kserve))
	if err != nil {
		return fmt.Errorf("error deleting application %v", tc.testDsc.Spec.Components.Kserve)
	}

	err = tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.Workbenches))
	if err != nil {
		return fmt.Errorf("error deleting application %v", tc.testDsc.Spec.Components.Workbenches)
	}

	err = tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.DataSciencePipelines))
	if err != nil {
		return fmt.Errorf("error deleting application %v", tc.testDsc.Spec.Components.DataSciencePipelines)
	}

	err = tc.testApplicationDeletion(&(tc.testDsc.Spec.Components.DistributeWorkloads))
	if err != nil {
		return fmt.Errorf("error deleting application %v", tc.testDsc.Spec.Components.DistributeWorkloads)
	}
	return nil
}
