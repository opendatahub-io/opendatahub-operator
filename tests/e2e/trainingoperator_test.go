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

	componentsv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

type TrainingOperatorTestCtx struct {
	testCtx                      *testContext
	testTrainingOperatorInstance componentsv1alpha1.TrainingOperator
}

func trainingoperatorTestSuite(t *testing.T) {
	t.Helper()

	trainingoperatorCtx := TrainingOperatorTestCtx{}
	var err error
	trainingoperatorCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := trainingoperatorCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// creation
		t.Run("Creation of TrainingOperator CR", func(t *testing.T) {
			err = trainingoperatorCtx.testTrainingOperatorCreation()
			require.NoError(t, err, "error creating TrainingOperator CR")
		})

		t.Run("Validate TrainingOperator instance", func(t *testing.T) {
			err = trainingoperatorCtx.validateTrainingOperator()
			require.NoError(t, err, "error validating TrainingOperator instance")
		})

		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = trainingoperatorCtx.testOwnerReferences()
			require.NoError(t, err, "error getting all TrainingOperator's Ownerrefrences")
		})

		t.Run("Validate TrainingOperator Ready", func(t *testing.T) {
			err = trainingoperatorCtx.validateTrainingOperatorReady()
			require.NoError(t, err, "TrainingOperator instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			err = trainingoperatorCtx.testUpdateOnTrainingOperatorResources()
			require.NoError(t, err, "error testing updates for TrainingOperator's managed resources")
		})

		t.Run("Validate Disabling TrainingOperator Component", func(t *testing.T) {
			err = trainingoperatorCtx.testUpdateTrainingOperatorComponentDisabled()
			require.NoError(t, err, "error testing TrainingOperator component enabled field")
		})
	})
}

func (tc *TrainingOperatorTestCtx) testTrainingOperatorCreation() error {
	if tc.testCtx.testDsc.Spec.Components.TrainingOperator.ManagementState != operatorv1.Managed {
		return nil
	}

	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		existingTrainingOperatorList := &componentsv1alpha1.TrainingOperatorList{}

		if err := tc.testCtx.customClient.List(ctx, existingTrainingOperatorList); err != nil {
			return false, err
		}

		switch {
		case len(existingTrainingOperatorList.Items) == 1:
			tc.testTrainingOperatorInstance = existingTrainingOperatorList.Items[0]
			return true, nil
		case len(existingTrainingOperatorList.Items) > 1:
			return false, fmt.Errorf(
				"unexpected TrainingOperator CR instances. Expected 1 , Found %v instance", len(existingTrainingOperatorList.Items))
		default:
			return false, nil
		}
	})

	if err != nil {
		return fmt.Errorf("unable to find TrainingOperator CR instance: %w", err)
	}

	return nil
}

func (tc *TrainingOperatorTestCtx) validateTrainingOperator() error {
	// TrainingOperator spec should match the spec of TrainingOperator component in DSC
	if !reflect.DeepEqual(tc.testCtx.testDsc.Spec.Components.TrainingOperator.TrainingOperatorCommonSpec, tc.testTrainingOperatorInstance.Spec.TrainingOperatorCommonSpec) {
		err := fmt.Errorf("expected .spec for TrainingOperator %v, got %v",
			tc.testCtx.testDsc.Spec.Components.TrainingOperator.TrainingOperatorCommonSpec, tc.testTrainingOperatorInstance.Spec.TrainingOperatorCommonSpec)
		return err
	}
	return nil
}

func (tc *TrainingOperatorTestCtx) testOwnerReferences() error {
	if len(tc.testTrainingOperatorInstance.OwnerReferences) != 1 {
		return errors.New("expect CR has ownerreferences set")
	}

	// Test TrainingOperator CR ownerref
	if tc.testTrainingOperatorInstance.OwnerReferences[0].Kind != dscKind {
		return fmt.Errorf("expected ownerreference DataScienceCluster not found. Got ownereferrence: %v",
			tc.testTrainingOperatorInstance.OwnerReferences[0].Kind)
	}

	// Test TrainingOperator resources
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.Component(componentsv1alpha1.TrainingOperatorComponentName),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}
	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != componentsv1alpha1.TrainingOperatorKind {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify TrainingOperator instance is in Ready phase when TrainingOperator deployments are up and running.
func (tc *TrainingOperatorTestCtx) validateTrainingOperatorReady() error {
	err := wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testTrainingOperatorInstance.Name}
		trainingoperator := &componentsv1alpha1.TrainingOperator{}

		err := tc.testCtx.customClient.Get(ctx, key, trainingoperator)
		if err != nil {
			return false, err
		}
		return trainingoperator.Status.Phase == readyStatus, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting Ready state for TrainingOperator %v: %w", tc.testTrainingOperatorInstance.Name, err)
	}

	return nil
}

func (tc *TrainingOperatorTestCtx) testUpdateOnTrainingOperatorResources() error {
	// Test Updating TrainingOperator Replicas

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(tc.testTrainingOperatorInstance.Kind),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 1 {
		return fmt.Errorf("error getting deployment for component %s", tc.testTrainingOperatorInstance.Name)
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

func (tc *TrainingOperatorTestCtx) testUpdateTrainingOperatorComponentDisabled() error {
	// Test Updating TrainingOperator to be disabled
	var trainingoperatorDeploymentName string

	if tc.testCtx.testDsc.Spec.Components.TrainingOperator.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component(componentsv1alpha1.TrainingOperatorComponentName),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", componentsv1alpha1.TrainingOperatorComponentName)
		}
		if len(appDeployments.Items) > 0 {
			trainingoperatorDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", trainingoperatorDeploymentName)
			}
		}
	} else {
		return errors.New("TrainingOperator spec should be in 'enabled: true' state in order to perform test")
	}

	// Disable component TrainingOperator
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh DSC instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable the Component
		tc.testCtx.testDsc.Spec.Components.TrainingOperator.ManagementState = operatorv1.Removed

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
		// Verify TrainingOperator CR is deleted
		trainingoperator := &componentsv1alpha1.TrainingOperator{}
		err = tc.testCtx.customClient.Get(ctx, client.ObjectKey{Name: tc.testTrainingOperatorInstance.Name}, trainingoperator)
		return k8serr.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("component TrainingOperator is disabled, should not get the TrainingOperator CR %v", tc.testTrainingOperatorInstance.Name)
	}

	// Sleep for 20 seconds to allow the operator to reconcile
	time.Sleep(2 * generalRetryInterval)
	_, err = tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).Get(tc.testCtx.ctx, trainingoperatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		componentsv1alpha1.TrainingOperatorKind,
		trainingoperatorDeploymentName,
		tc.testCtx.applicationsNamespace)
}
