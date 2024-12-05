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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

type RayTestCtx struct {
	testCtx         *testContext
	testRayInstance componentApi.Ray
}

func rayTestSuite(t *testing.T) {
	t.Helper()

	rayCtx := RayTestCtx{}
	var err error
	rayCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := rayCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// creation
		t.Run("Creation of Ray CR", func(t *testing.T) {
			err = rayCtx.testRayCreation()
			require.NoError(t, err, "error creating Ray CR")
		})

		t.Run("Validate Ray instance", func(t *testing.T) {
			err = rayCtx.validateRay()
			require.NoError(t, err, "error validating Ray instance")
		})

		t.Run("Validate Ownerreferences exist", func(t *testing.T) {
			err = rayCtx.testOwnerReferences()
			require.NoError(t, err, "error getting all Ray's Ownerrefrences")
		})

		t.Run("Validate Ray Ready", func(t *testing.T) {
			err = rayCtx.validateRayReady()
			require.NoError(t, err, "Ray instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			err = rayCtx.testUpdateOnRayResources()
			require.NoError(t, err, "error testing updates for Ray's managed resources")
		})

		t.Run("Validate Disabling Ray Component", func(t *testing.T) {
			err = rayCtx.testUpdateRayComponentDisabled()
			require.NoError(t, err, "error testing ray component enabled field")
		})
	})
}

func (tc *RayTestCtx) testRayCreation() error {
	if tc.testCtx.testDsc.Spec.Components.Ray.ManagementState != operatorv1.Managed {
		return nil
	}

	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		existingRayList := &componentApi.RayList{}

		if err := tc.testCtx.customClient.List(ctx, existingRayList); err != nil {
			return false, err
		}

		switch {
		case len(existingRayList.Items) == 1:
			tc.testRayInstance = existingRayList.Items[0]
			return true, nil
		case len(existingRayList.Items) > 1:
			return false, fmt.Errorf(
				"unexpected Ray CR instances. Expected 1 , Found %v instance", len(existingRayList.Items))
		default:
			return false, nil
		}
	})

	if err != nil {
		return fmt.Errorf("unable to find Ray CR instance: %w", err)
	}

	return nil
}

func (tc *RayTestCtx) validateRay() error {
	// Ray spec should match the spec of Ray component in DSC
	if !reflect.DeepEqual(tc.testCtx.testDsc.Spec.Components.Ray.RayCommonSpec, tc.testRayInstance.Spec.RayCommonSpec) {
		err := fmt.Errorf("expected .spec for Ray %v, got %v",
			tc.testCtx.testDsc.Spec.Components.Ray.RayCommonSpec, tc.testRayInstance.Spec.RayCommonSpec)
		return err
	}
	return nil
}

func (tc *RayTestCtx) testOwnerReferences() error {
	if len(tc.testRayInstance.OwnerReferences) != 1 {
		return errors.New("expect CR has ownerreferences set")
	}

	// Test Ray CR ownerref
	if tc.testRayInstance.OwnerReferences[0].Kind != dscKind {
		return fmt.Errorf("expected ownerreference DataScienceCluster not found. Got ownereferrence: %v",
			tc.testRayInstance.OwnerReferences[0].Kind)
	}

	// Test Ray resources
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.Component(componentApi.RayComponentName),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}
	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != componentApi.RayKind {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify Ray instance is in Ready phase when ray deployments are up and running.
func (tc *RayTestCtx) validateRayReady() error {
	err := wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testRayInstance.Name}
		ray := &componentApi.Ray{}

		err := tc.testCtx.customClient.Get(ctx, key, ray)
		if err != nil {
			return false, err
		}
		return ray.Status.Phase == readyStatus, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting Ready state for Ray %v: %w", tc.testRayInstance.Name, err)
	}

	return nil
}

func (tc *RayTestCtx) testUpdateOnRayResources() error {
	// Test Updating Ray Replicas

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(tc.testRayInstance.Kind),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 1 {
		return fmt.Errorf("error getting deployment for component %s", tc.testRayInstance.Name)
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

func (tc *RayTestCtx) testUpdateRayComponentDisabled() error {
	// Test Updating Ray to be disabled
	var rayDeploymentName string

	if tc.testCtx.testDsc.Spec.Components.Ray.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component(componentApi.RayComponentName),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", componentApi.RayComponentName)
		}
		if len(appDeployments.Items) > 0 {
			rayDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", rayDeploymentName)
			}
		}
	} else {
		return errors.New("ray spec should be in 'enabled: true' state in order to perform test")
	}

	// Disable component Ray
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh DSC instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable the Component
		tc.testCtx.testDsc.Spec.Components.Ray.ManagementState = operatorv1.Removed

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
		// Verify ray CR is deleted
		ray := &componentApi.Ray{}
		err = tc.testCtx.customClient.Get(ctx, client.ObjectKey{Name: tc.testRayInstance.Name}, ray)
		return k8serr.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("component ray is disabled, should not get the Ray CR %v", tc.testRayInstance.Name)
	}

	// Sleep for 20 seconds to allow the operator to reconcile
	time.Sleep(2 * generalRetryInterval)
	_, err = tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).Get(tc.testCtx.ctx, rayDeploymentName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		componentApi.RayKind,
		rayDeploymentName,
		tc.testCtx.applicationsNamespace)
}
