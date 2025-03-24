package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

type WorkbenchesTestCtx struct {
	testCtx                 *testContext
	testWorkbenchesInstance componentApi.Workbenches
}

func workbenchesTestSuite(t *testing.T) {
	t.Helper()

	workbenchesCtx := WorkbenchesTestCtx{}
	var err error
	workbenchesCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := workbenchesCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Creation of Workbenches CR", func(t *testing.T) {
			err = workbenchesCtx.testWorkbenchesCreation()
			require.NoError(t, err, "error creating Workbenches CR")
		})

		t.Run("Validate Workbenches instance", func(t *testing.T) {
			err = workbenchesCtx.validateWorkbenches()
			require.NoError(t, err, "error validating Workbenches instance")
		})

		t.Run("Validate Ownerreferences exist", func(t *testing.T) {
			err = workbenchesCtx.testOwnerReferences()
			require.NoError(t, err, "error getting all Workbenches's Ownerreferences")
		})

		t.Run("Validate Workbenches Ready", func(t *testing.T) {
			err = workbenchesCtx.validateWorkbenchesReady()
			require.NoError(t, err, "Workbenches instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			err = workbenchesCtx.testUpdateOnWorkbenchesResources()
			require.NoError(t, err, "error testing updates for Workbenches' managed resources")
		})

		t.Run("Validate Disabling Component", func(t *testing.T) {
			err = workbenchesCtx.testUpdateWorkbenchesComponentDisabled()
			require.NoError(t, err, "error testing component enabled field")
		})
	})
}

func (tc *WorkbenchesTestCtx) testWorkbenchesCreation() error {
	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		key := client.ObjectKeyFromObject(tc.testCtx.testDsc)

		err := tc.testCtx.customClient.Get(ctx, key, tc.testCtx.testDsc)
		if err != nil {
			return false, fmt.Errorf("error getting resource %w", err)
		}

		tc.testCtx.testDsc.Spec.Components.Workbenches.ManagementState = operatorv1.Managed

		switch err = tc.testCtx.customClient.Update(ctx, tc.testCtx.testDsc); {
		case err == nil:
			return true, nil
		case k8serr.IsConflict(err):
			return false, nil
		default:
			return false, fmt.Errorf("error updating resource %w", err)
		}
	})
	if err != nil {
		return fmt.Errorf("error after retry %w", err)
	}

	err = tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		existingWorkbenchesList := &componentApi.WorkbenchesList{}

		err := tc.testCtx.customClient.List(ctx, existingWorkbenchesList)
		if err != nil {
			return false, err
		}

		switch {
		case len(existingWorkbenchesList.Items) == 1:
			tc.testWorkbenchesInstance = existingWorkbenchesList.Items[0]
			return true, nil

		case len(existingWorkbenchesList.Items) > 1:
			return false, fmt.Errorf(
				"unexpected Workbenches CR instances. Expected 1 , Found %v instance", len(existingWorkbenchesList.Items))
		default:
			return false, nil
		}
	})

	if err != nil {
		return fmt.Errorf("unable to find Workbenches CR instance: %w", err)
	}

	return nil
}

func (tc *WorkbenchesTestCtx) validateWorkbenches() error {
	// Workbenches spec should match the spec of Workbenches component in DSC
	if !reflect.DeepEqual(tc.testCtx.testDsc.Spec.Components.Workbenches.WorkbenchesCommonSpec, tc.testWorkbenchesInstance.Spec.WorkbenchesCommonSpec) {
		err := fmt.Errorf("expected spec for Workbenches %v, got %v",
			tc.testCtx.testDsc.Spec.Components.Workbenches.WorkbenchesCommonSpec, tc.testWorkbenchesInstance.Spec.WorkbenchesCommonSpec)
		return err
	}
	return nil
}

func (tc *WorkbenchesTestCtx) testOwnerReferences() error {
	if len(tc.testWorkbenchesInstance.OwnerReferences) != 1 {
		return errors.New("expect CR has ownerreferences set")
	}

	// Test Workbenches CR ownerref
	if tc.testWorkbenchesInstance.OwnerReferences[0].Kind != dscKind {
		return fmt.Errorf("expected ownerreference DataScienceCluster not found. Got ownerreferrence: %v",
			tc.testWorkbenchesInstance.OwnerReferences[0].Kind)
	}

	// Test Workbenches resources

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.Workbenches.Kind),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}
	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != componentApi.WorkbenchesKind {
		return fmt.Errorf("expected ownerreference not found. Got ownerreferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify Workbenches instance is in Ready phase when Workbenches deployments are up and running.
func (tc *WorkbenchesTestCtx) validateWorkbenchesReady() error {
	err := wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testWorkbenchesInstance.Name}
		wb := &componentApi.Workbenches{}

		err := tc.testCtx.customClient.Get(ctx, key, wb)
		if err != nil {
			return false, err
		}
		return wb.Status.Phase == readyStatus, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting on Ready state for Workbenches %v: %w", tc.testWorkbenchesInstance.Name, err)
	}

	err = wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		list := dscv1.DataScienceClusterList{}
		err := tc.testCtx.customClient.List(ctx, &list)
		if err != nil {
			return false, err
		}

		if len(list.Items) != 1 {
			return false, fmt.Errorf("expected 1 DataScience Cluster CR but found %v", len(list.Items))
		}

		for _, c := range list.Items[0].Status.Conditions {
			if c.Type == workbenches.ReadyConditionType {
				return c.Status == metav1.ConditionTrue, nil
			}
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting on Ready state for Workbenches component in DSC: %w", err)
	}

	return nil
}

func (tc *WorkbenchesTestCtx) testUpdateOnWorkbenchesResources() error {
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(tc.testWorkbenchesInstance.Kind),
	})
	if err != nil {
		return err
	}

	// expects odh-notebook-controller-manager and notebook-controller-deployment deployments
	if len(appDeployments.Items) != 2 {
		return fmt.Errorf("error getting deployment for component %s", tc.testWorkbenchesInstance.Name)
	}

	const expectedReplica int32 = 2

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
	updatedDep, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).UpdateScale(tc.testCtx.ctx,
		testDeployment.Name, patchedReplica, metav1.UpdateOptions{})
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

func (tc *WorkbenchesTestCtx) testUpdateWorkbenchesComponentDisabled() error {
	if tc.testCtx.testDsc.Spec.Components.Workbenches.ManagementState != operatorv1.Managed {
		return errors.New("the Workbenches spec should be in 'enabled: true' state in order to perform test")
	}

	deployments, err := tc.testCtx.getComponentDeployments(gvk.Workbenches)
	if err != nil {
		return fmt.Errorf("error getting enabled component %s", componentApi.WorkbenchesComponentName)
	}

	for _, d := range deployments {
		if d.Status.ReadyReplicas == 0 {
			return fmt.Errorf("component %s deployment %sis not ready", d.Name, componentApi.WorkbenchesComponentName)
		}
	}

	// Disable component Workbenches
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh the instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable the Component
		tc.testCtx.testDsc.Spec.Components.Workbenches.ManagementState = operatorv1.Removed

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

	err = tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		// Verify Workbenches CR is deleted
		wb := &componentApi.Workbenches{}
		err = tc.testCtx.customClient.Get(ctx, client.ObjectKey{Name: tc.testWorkbenchesInstance.Name}, wb)
		return k8serr.IsNotFound(err), nil
	})

	if err != nil {
		return fmt.Errorf("component %v is disabled, should not get the Workbenches CR %v", tc.testWorkbenchesInstance.Name, tc.testWorkbenchesInstance.Name)
	}

	deployments, err = tc.testCtx.getComponentDeployments(gvk.Workbenches)
	if err != nil {
		return fmt.Errorf("error listing deployments: %w", err)
	}

	if len(deployments) != 0 {
		return fmt.Errorf("component %v is disabled, should not have deployments in NS %v any more",
			gvk.Workbenches.Kind,
			tc.testCtx.applicationsNamespace)
	}

	return nil
}
