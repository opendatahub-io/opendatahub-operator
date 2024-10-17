package e2e_test

import (
	"context"
	"errors"
	"fmt"
	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
	"time"
)

type DashboardTestCtx struct {
	testCtx               *testContext
	testDashboardInstance componentsv1.Dashboard
}

func dashboardTestSuite(t *testing.T) {
	dashboardCtx := DashboardTestCtx{}
	var err error
	dashboardCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := dashboardCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// DSCI
		t.Run("Creation of Dashboard CR", func(t *testing.T) {
			err = dashboardCtx.testDashboardCreation()
			require.NoError(t, err, "error creating DSCI CR")
		})

		t.Run("Validate Dashboard instance", func(t *testing.T) {
			err = dashboardCtx.validateDashboard()
			require.NoError(t, err, "error validating Dashboard instance")
		})

		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = dashboardCtx.testOwnerrefrences()
			require.NoError(t, err, "error getting all Dashboard's Ownerrefrences")
		})

		t.Run("Validate Dashboard Ready", func(t *testing.T) {
			err = dashboardCtx.validateDashboardReady()
			require.NoError(t, err, "Dashboard instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			// only test Dashboard component for now
			err = dashboardCtx.testUpdateOnDashboardResources()
			require.NoError(t, err, "error testing updates for Dashboard's managed resources")
		})
		// TODO: Uncomment this when we set up component removed logic
		//t.Run("Validate Disabling Component", func(t *testing.T) {
		//	err = dashboardCtx.testUpdateDashboardComponentDisabled()
		//	require.NoError(t, err, "error testing component enabled field")
		//})
	})
}

func (tc *DashboardTestCtx) testDashboardCreation() error {
	existingDashboardList := &componentsv1.DashboardList{}

	if tc.testCtx.testDsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
		err := tc.testCtx.customClient.List(tc.testCtx.ctx, existingDashboardList)
		if err == nil {
			if len(existingDashboardList.Items) == 1 {
				tc.testDashboardInstance = existingDashboardList.Items[0]
				return nil
			} else {
				return errors.New(fmt.Sprintf("Unexpected Dashboard CR instances. Expected 1 , "+
					"Found %v instance", len(existingDashboardList.Items)))
			}
		}
	}
	return nil
}

func (tc *DashboardTestCtx) validateDashboard() error {

	// Dashboard spec should match the spec of Dashboard component in DSC
	if !reflect.DeepEqual(tc.testCtx.testDsc.Spec.Components.Dashboard.ManagementState, tc.testDashboardInstance.Spec.ManagementState) {
		err := fmt.Errorf("expected smanagement state for Dashboard %v, got %v",
			tc.testCtx.testDsc.Spec.Components.Dashboard.ManagementState, tc.testDashboardInstance.Spec.ManagementState)
		return err
	}
	return nil
}

func (tc *DashboardTestCtx) testOwnerrefrences() error {
	// Test Dashboard CR ownerref
	if tc.testDashboardInstance.OwnerReferences[0].Kind != "DataScienceCluster" {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			tc.testDashboardInstance.OwnerReferences[0].Kind)
	}

	// Test Dashboard resources

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.Component("dashboard"),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}
	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != "Dashboard" {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify Dashboard instance is in Ready phase when dashboard deployments are up and running
func (tc *DashboardTestCtx) validateDashboardReady() error {
	// wait for 2 mins which is on the safe side, normally it should get ready once all components are ready
	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testDashboardInstance.Name}
		dashboard := &componentsv1.Dashboard{}

		err := tc.testCtx.customClient.Get(ctx, key, dashboard)
		if err != nil {
			return false, err
		}
		return dashboard.Status.Phase == "Ready", nil
	})

	if err != nil {
		return fmt.Errorf("error waiting Ready state for Dashboard %v: %w", tc.testDashboardInstance.Name, err)
	}

	return nil
}

func (tc *DashboardTestCtx) testUpdateOnDashboardResources() error {
	// Test Updating Dashboard Replicas

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.Component("dashboard"),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 1 {
		return fmt.Errorf("error getting deployment for component %s", "dashboard")
	}

	const expectedReplica int32 = 3

	testDeployment := appDeployments.Items[0]
	patchedReplica := &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDeployment.Name,
			Namespace: testDeployment.Namespace,
		},
		Spec: autoscalingv1.ScaleSpec{
			Replicas: expectedReplica,
		},
		Status: autoscalingv1.ScaleStatus{},
	}
	updatedDep, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).UpdateScale(tc.testCtx.ctx, testDeployment.Name, patchedReplica, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error patching component resources : %w", err)
	}
	if updatedDep.Spec.Replicas != patchedReplica.Spec.Replicas {
		return fmt.Errorf("failed to patch replicas : expect to be %v but got %v", patchedReplica.Spec.Replicas, updatedDep.Spec.Replicas)
	}

	// Sleep for 40 seconds to allow the operator to reconcile
	// we expect it should not revert back to original value because of AllowList
	time.Sleep(4 * generalRetryInterval)
	reconciledDep, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).Get(tc.testCtx.ctx, testDeployment.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	if *reconciledDep.Spec.Replicas != expectedReplica {
		return fmt.Errorf("failed to revert back replicas : expect to be %v but got %v", expectedReplica, *reconciledDep.Spec.Replicas)
	}

	return nil
}

func (tc *DashboardTestCtx) testUpdateDashboardComponentDisabled() error {
	// Test Updating dashboard to be disabled
	var dashboardDeploymentName string

	if tc.testCtx.testDsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component("dashboard"),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", "dashboard")
		}
		if len(appDeployments.Items) > 0 {
			dashboardDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", dashboardDeploymentName)
			}
		}
	} else {
		return errors.New("dashboard spec should be in 'enabled: true' state in order to perform test")
	}

	// Disable component Dashboard
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh the instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable the Component
		tc.testCtx.testDsc.Spec.Components.Dashboard.ManagementState = operatorv1.Removed

		// Try to update
		err = tc.testCtx.customClient.Update(tc.testCtx.ctx, tc.testCtx.testDsc)
		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		if err != nil {
			return fmt.Errorf("error updating component from 'enabled: true' to 'enabled: false': %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error after retry %w", err)
	}

	// Verify dashboard CR is deleted
	dashboard := &componentsv1.Dashboard{}
	err = tc.testCtx.customClient.Get(tc.testCtx.ctx, client.ObjectKey{Name: tc.testDashboardInstance.Name}, dashboard)
	if err == nil {
		return errors.New(fmt.Sprintf("component %v is disabled, should not get the Dashboard CR %v", "dashboard", tc.testDashboardInstance.Name))
	} else if !k8serr.IsNotFound(err) {
		return err
	}

	// Sleep for 20 seconds to allow the operator to reconcile
	time.Sleep(2 * generalRetryInterval)
	_, err = tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).Get(tc.testCtx.ctx, dashboardDeploymentName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		"dashboard",
		dashboardDeploymentName,
		tc.testCtx.applicationsNamespace)
}
