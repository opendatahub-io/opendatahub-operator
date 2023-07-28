package e2e

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"k8s.io/client-go/util/retry"

	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
)

func creationTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)
	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Creation of DataScienceCluster instance", func(t *testing.T) {
			err = testCtx.testDSCCreation()
			require.NoError(t, err, "error creating DataScienceCluster object ")
		})
		t.Run("Validate deployed applications", func(t *testing.T) {
			err = testCtx.testAllDSCApplications()
			require.NoError(t, err, "error testing deployments for application %v", testCtx.testDsc.Name)

		})
		t.Run("Validate Ownerrefrences are added", func(t *testing.T) {
			err = testCtx.testOwnerrefrences()
			require.NoError(t, err, "error testing DSC Ownerrefrences ")
		})
		t.Run("Validate Controller Reverts Updates", func(t *testing.T) {
			err = testCtx.testUpdateManagedResource()
			require.NoError(t, err, "error testing updates for DSC managed resource ")
		})
		t.Run("Validate Component Enabled field", func(t *testing.T) {
			err = testCtx.testUpdatingComponentEnabledField()
			require.NoError(t, err, "error testing component enabled field")
		})

	})

}

func (tc *testContext) testDSCCreation() error {

	// Create DataScienceCluster resource if not already created
	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	createdDSC := &dsc.DataScienceCluster{}
	existingDSCList := &dsc.DataScienceClusterList{}

	err := tc.customClient.List(tc.ctx, existingDSCList)
	if err == nil {
		if len(existingDSCList.Items) > 0 {
			// Use DSC instance if it already exists
			tc.testDsc = &existingDSCList.Items[0]
			return nil
		}
	}

	err = tc.customClient.Get(tc.ctx, dscLookupKey, createdDSC)
	if err != nil {
		if errors.IsNotFound(err) {
			nberr := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (done bool, err error) {
				creationErr := tc.customClient.Create(tc.ctx, tc.testDsc)
				if creationErr != nil {
					log.Printf("Error creating DSC resource %v: %v, trying again",
						tc.testDsc.Name, creationErr)
					return false, nil
				} else {
					return true, nil
				}
			})
			if nberr != nil {
				return fmt.Errorf("error creating test DSC %s: %v", tc.testDsc.Name, nberr)
			}
		} else {
			return fmt.Errorf("error getting test DSC %s: %v", tc.testDsc.Name, err)
		}
	}
	return nil
}

func (tc *testContext) testAllDSCApplications() error {
	// Validate test instance is in Ready state
	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	createdDSC := &dsc.DataScienceCluster{}

	// Wait for applications to get deployed
	time.Sleep(1 * time.Minute)

	err := tc.customClient.Get(tc.ctx, dscLookupKey, createdDSC)
	if err != nil {
		return fmt.Errorf("error getting DataScienceCluster instance :%v", tc.testDsc.Name)
	}
	tc.testDsc = createdDSC

	// Verify DSC instance is in Ready phase
	if tc.testDsc.Status.Phase != "Ready" {
		return fmt.Errorf("DSC instance is not in Ready phase. Current phase: %v", tc.testDsc.Status.Phase)

	}
	// Verify every component status matches the given enable value

	err = tc.testDSCApplication(&(tc.testDsc.Spec.Components.Dashboard))
	// enabled applications should not have any error
	if tc.testDsc.Spec.Components.Dashboard.Enabled {
		if err != nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.Dashboard)
		}
	} else {
		if err == nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.Dashboard)
		}
	}

	err = tc.testDSCApplication(&(tc.testDsc.Spec.Components.ModelMeshServing))
	if tc.testDsc.Spec.Components.ModelMeshServing.Enabled {
		if err != nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.ModelMeshServing)
		}
	} else {
		if err == nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.ModelMeshServing)
		}
	}

	err = tc.testDSCApplication(&(tc.testDsc.Spec.Components.Kserve))
	if tc.testDsc.Spec.Components.Kserve.Enabled {
		if err != nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.Kserve)
		}
	} else {
		if err == nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.Kserve)
		}
	}

	err = tc.testDSCApplication(&(tc.testDsc.Spec.Components.Workbenches))
	if tc.testDsc.Spec.Components.Workbenches.Enabled {
		if err != nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.Workbenches)
		}
	} else {
		if err == nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.Workbenches)
		}
	}

	err = tc.testDSCApplication(&(tc.testDsc.Spec.Components.DataSciencePipelines))
	if tc.testDsc.Spec.Components.DataSciencePipelines.Enabled {
		if err != nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.DataSciencePipelines)
		}
	} else {
		if err == nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.DataSciencePipelines)
		}
	}

	err = tc.testDSCApplication(&(tc.testDsc.Spec.Components.CodeFlare))
	if tc.testDsc.Spec.Components.CodeFlare.Enabled {
		if err != nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.CodeFlare)
		}
	} else {
		if err == nil {
			return fmt.Errorf("error validating application %v", tc.testDsc.Spec.Components.CodeFlare)
		}
	}
	return nil
}

func (tc *testContext) testDSCApplication(component components.ComponentInterface) error {
	err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (done bool, err error) {
		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/part-of=" + component.GetComponentName(),
		})
		if err != nil {
			log.Printf("error listing application deployments :%v. Trying again...", err)
			return false, fmt.Errorf("error listing application deployments :%v. Trying again", err)
		}
		if len(appList.Items) != 0 {
			allAppDeploymentsReady := true
			for _, deployment := range appList.Items {
				if deployment.Status.ReadyReplicas < 1 {
					allAppDeploymentsReady = false
				}
			}
			if allAppDeploymentsReady {
				return true, nil
			} else {
				log.Printf("waiting for application deployments to be in Ready state.")
				return false, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return err
}

func (tc *testContext) testOwnerrefrences() error {
	// Test any one of the apps
	if tc.testDsc.Spec.Components.Dashboard.Enabled {

		appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/part-of=" + tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
		})
		if err != nil {
			return fmt.Errorf("error listing application deployments %v", err)
		} else {
			// test any one deployment for ownerreference
			if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != "DataScienceCluster" {

				return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
					appDeployments.Items[0].OwnerReferences)
			}
		}
		return nil
	}
	return nil
}

func (tc *testContext) testUpdateManagedResource() error {
	// Test Updating Dashboard Replicas

	appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/part-of=" + tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
	})

	if err != nil {
		return err
	}
	if len(appDeployments.Items) != 0 {
		testDeployment := appDeployments.Items[0]
		expectedReplica := testDeployment.Spec.Replicas
		patchedReplica := &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testDeployment.Name,
				Namespace: testDeployment.Namespace,
			},
			Spec: autoscalingv1.ScaleSpec{
				Replicas: 3,
			},
			Status: autoscalingv1.ScaleStatus{},
		}
		retrievedDep, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).UpdateScale(context.TODO(), testDeployment.Name, patchedReplica, metav1.UpdateOptions{})
		if err != nil {

			return fmt.Errorf("error patching application resources : %v", err)
		}
		if retrievedDep.Spec.Replicas != patchedReplica.Spec.Replicas {
			return fmt.Errorf("failed to patch replicas")

		}
		// Sleep for 20 seconds to allow the operator to reconcile
		time.Sleep(2 * tc.resourceRetryInterval)
		revertedDep, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).Get(context.TODO(), testDeployment.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error getting application resource : %v", err)
		}

		if *revertedDep.Spec.Replicas != *expectedReplica {
			return fmt.Errorf("failed to revert updated resource")
		}
	}

	return nil
}

func (tc *testContext) testUpdatingComponentEnabledField() error {
	// Update any component to be disabled
	var dashboardDeploymentName string
	if tc.testDsc.Spec.Components.Dashboard.Enabled {
		appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/part-of=" + tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", tc.testDsc.Spec.Components.Dashboard.GetComponentName())
		}
		if len(appDeployments.Items) > 0 {
			dashboardDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting ready replicas of enabled component: %v", dashboardDeploymentName)
			}
		}
	}

	// Disable component Dashboard
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh the instance in case it was updated during the reconcile
		err := tc.customClient.Get(tc.ctx, types.NamespacedName{Name: tc.testDsc.Name}, tc.testDsc)
		if err != nil {
			return err
		}
		// Disable the Component
		tc.testDsc.Spec.Components.Dashboard.Enabled = false

		// Try to update
		err = tc.customClient.Update(context.TODO(), tc.testDsc)
		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return err
	})
	if err != nil {
		return fmt.Errorf("error updating component from enabled true to false %v", err)
	}

	// Sleep for 20 seconds to allow the operator to reconcile
	time.Sleep(2 * tc.resourceRetryInterval)
	_, err = tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).Get(context.TODO(), dashboardDeploymentName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	} else {
		return fmt.Errorf("component %v not disabled", tc.testDsc.Spec.Components.Dashboard.GetComponentName())
	}
}
