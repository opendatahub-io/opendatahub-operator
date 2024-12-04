package e2e_test

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

type ModelControllerTestCtx struct {
	testCtx                     *testContext
	testModelControllerInstance componentApi.ModelController
}

func modelControllerTestSuite(t *testing.T) {
	t.Helper()

	mcCtx := ModelControllerTestCtx{}
	var err error
	mcCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := mcCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// creation
		t.Run("Check ModelController CR exist", func(t *testing.T) {
			err = mcCtx.testModelControllerAvaile()
			require.NoError(t, err, "error creating ModelController CR")
		})

		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = mcCtx.testOwnerReferences()
			require.NoError(t, err, "error getting ModelController's Ownerrefrences")
		})

		t.Run("Validate ModelController Ready", func(t *testing.T) {
			err = mcCtx.validateModelControllerReady()
			require.NoError(t, err, "ModelController instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			err = mcCtx.testUpdateOnModelControllerResources()
			require.NoError(t, err, "error testing updates for ModelController's managed resources")
		})

		t.Run("Validate Disabling modelmesh and kserve Component then ModelController is removed", func(t *testing.T) {
			err = mcCtx.testUpdateModelControllerComponentDisabled()
			require.NoError(t, err, "error testing modemeshserving component enabled field")
		})
	})
}

func (tc *ModelControllerTestCtx) testModelControllerAvaile() error {
	// force to set modelmesh
	err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
	if err != nil {
		return fmt.Errorf("error getting DSC %w", err)
	}
	if tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ManagementState != operatorv1.Managed {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
			if err != nil {
				return fmt.Errorf("error getting DSC %w", err)
			}
			// Enable the DSC ModelMeshServing
			tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ManagementState = operatorv1.Managed

			// Try to update
			err = tc.testCtx.customClient.Update(tc.testCtx.ctx, tc.testCtx.testDsc)
			if err != nil {
				return fmt.Errorf("error updating DSC from removed to managed: %w", err)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("error after retry %w", err)
		}
	}
	err = tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
	if err != nil {
		return fmt.Errorf("error getting DSC resource again %w", err)
	}

	err = tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		existingModelControllerList := &componentApi.ModelControllerList{}

		if err := tc.testCtx.customClient.List(ctx, existingModelControllerList); err != nil {
			return false, err
		}

		switch {
		case len(existingModelControllerList.Items) == 1:
			tc.testModelControllerInstance = existingModelControllerList.Items[0]
			return true, nil
		case len(existingModelControllerList.Items) > 1:
			return false, fmt.Errorf(
				"unexpected ModelController CR instances. Expected 1 , Found %v instance", len(existingModelControllerList.Items))
		default:
			return false, nil
		}
	})

	if err != nil {
		return fmt.Errorf("unable to find ModelController CR instance: %w", err)
	}

	return nil
}

func (tc *ModelControllerTestCtx) testOwnerReferences() error {
	// Test ModelController CR ownerref
	if tc.testModelControllerInstance.OwnerReferences[0].Kind != dscKind {
		return fmt.Errorf("expected ownerreference DataScienceCluster not found. Got ownereferrence: %v",
			tc.testModelControllerInstance.OwnerReferences[0].Kind)
	}

	// Test ModelController resources
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.Component(componentApi.ModelControllerComponentName),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}
	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != componentApi.ModelControllerKind {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify ModelController instance is in Ready phase when ModelMesh deployments are up and running.
func (tc *ModelControllerTestCtx) validateModelControllerReady() error {
	err := wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testModelControllerInstance.Name}
		mc := &componentApi.ModelController{}

		err := tc.testCtx.customClient.Get(ctx, key, mc)
		if err != nil {
			return false, err
		}
		return mc.Status.Phase == readyStatus, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting Ready state for ModelController %v: %w", tc.testModelControllerInstance.Name, err)
	}

	return nil
}

func (tc *ModelControllerTestCtx) testUpdateOnModelControllerResources() error {
	// Test Updating ModelController Replicas

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(componentApi.ModelControllerKind),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 1 {
		return fmt.Errorf("error getting deployment for component %s", tc.testModelControllerInstance.Name)
	}

	const expectedReplica int32 = 2 // from 1 to 2

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

	// Sleep for 20 seconds to allow the operator to reconcile
	// we expect it should not revert back to original value because of AllowList
	time.Sleep(2 * generalRetryInterval)
	reconciledDep, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).Get(tc.testCtx.ctx, testDeployment.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	if *reconciledDep.Spec.Replicas != expectedReplica {
		return fmt.Errorf("failed to revert back replicas : expect to be %v but got %v", expectedReplica, *reconciledDep.Spec.Replicas)
	}

	return nil
}

func (tc *ModelControllerTestCtx) testUpdateModelControllerComponentDisabled() error {
	// Test Updating ModelMesh and kserve to be disabled then ModelController is removed
	var mcDeploymentName string

	if tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component(componentApi.ModelControllerComponentName),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", componentApi.ModelControllerComponentName)
		}
		if len(appDeployments.Items) > 0 {
			mcDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", mcDeploymentName)
			}
		}
	} else {
		return errors.New("ModelMesh spec should be in 'enabled: true' state in order to perform modelcontroller test")
	}

	// Disable component ModleMeshServing
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh DSC instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable modelmesh and kserve Component
		tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ManagementState = operatorv1.Removed
		tc.testCtx.testDsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed

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

	if err = tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		// Verify ModelController CR is deleted
		mc := &componentApi.ModelController{}
		err = tc.testCtx.customClient.Get(ctx, client.ObjectKey{Name: tc.testModelControllerInstance.Name}, mc)
		return k8serr.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("component modemeshserving is disabled, should not get the ModelController CR %v", tc.testModelControllerInstance.Name)
	}

	// Sleep for 20 seconds to see if operator reconcile back modelcontroller cr and deployment
	time.Sleep(2 * generalRetryInterval)
	_, err = tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).Get(tc.testCtx.ctx, mcDeploymentName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		componentApi.ModelControllerKind,
		mcDeploymentName,
		tc.testCtx.applicationsNamespace)
}
