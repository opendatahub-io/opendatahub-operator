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

type ModelMeshServingTestCtx struct {
	testCtx                      *testContext
	testModelMeshServingInstance componentApi.ModelMeshServing
}

func modelMeshServingTestSuite(t *testing.T) {
	t.Helper()

	mmCtx := ModelMeshServingTestCtx{}
	var err error
	mmCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := mmCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// creation
		t.Run("Creation of ModelMeshServing CR", func(t *testing.T) {
			err = mmCtx.testModelMeshServingCreation()
			require.NoError(t, err, "error creating ModelMeshServing CR")
		})

		t.Run("Validate ModelMeshServing instance", func(t *testing.T) {
			err = mmCtx.validateModelMeshServing()
			require.NoError(t, err, "error validating ModelMeshServing instance")
		})

		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = mmCtx.testOwnerReferences()
			require.NoError(t, err, "error getting all ModelMeshServing's Ownerrefrences")
		})

		t.Run("Validate ModelMeshServing Ready", func(t *testing.T) {
			err = mmCtx.validateModelMeshServingReady()
			require.NoError(t, err, "ModelMeshServing instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			err = mmCtx.testUpdateOnModelMeshServingResources()
			require.NoError(t, err, "error testing updates for ModelMeshServing's managed resources")
		})

		t.Run("Validate Disabling ModelMeshServing Component", func(t *testing.T) {
			err = mmCtx.testUpdateModelMeshServingComponentDisabled()
			require.NoError(t, err, "error testing modemeshserving component enabled field")
		})
	})
}

func (tc *ModelMeshServingTestCtx) testModelMeshServingCreation() error {
	if tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ManagementState != operatorv1.Managed {
		return nil
	}

	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		existingModelMeshServingList := &componentApi.ModelMeshServingList{}

		if err := tc.testCtx.customClient.List(ctx, existingModelMeshServingList); err != nil {
			return false, err
		}

		switch {
		case len(existingModelMeshServingList.Items) == 1:
			tc.testModelMeshServingInstance = existingModelMeshServingList.Items[0]
			return true, nil
		case len(existingModelMeshServingList.Items) > 1:
			return false, fmt.Errorf(
				"unexpected ModelMeshServing CR instances. Expected 1 , Found %v instance", len(existingModelMeshServingList.Items))
		default:
			return false, nil
		}
	})

	if err != nil {
		return fmt.Errorf("unable to find ModelMeshServing CR instance: %w", err)
	}

	return nil
}

func (tc *ModelMeshServingTestCtx) validateModelMeshServing() error {
	// ModelMeshServing spec should match the spec of ModelMeshServing component in DSC
	if !reflect.DeepEqual(tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ModelMeshServingCommonSpec, tc.testModelMeshServingInstance.Spec.ModelMeshServingCommonSpec) {
		err := fmt.Errorf("expected .spec for ModelMeshServing %v, got %v",
			tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ModelMeshServingCommonSpec, tc.testModelMeshServingInstance.Spec.ModelMeshServingCommonSpec)
		return err
	}
	return nil
}

func (tc *ModelMeshServingTestCtx) testOwnerReferences() error {
	if len(tc.testModelMeshServingInstance.OwnerReferences) != 1 {
		return errors.New("expect CR has ownerreferences set")
	}

	// Test ModelMeshServing CR ownerref
	if tc.testModelMeshServingInstance.OwnerReferences[0].Kind != dscKind {
		return fmt.Errorf("expected ownerreference DataScienceCluster not found. Got ownereferrence: %v",
			tc.testModelMeshServingInstance.OwnerReferences[0].Kind)
	}

	// Test ModelMeshServing resources
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.Component(componentApi.ModelMeshServingComponentName),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}
	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != componentApi.ModelMeshServingKind {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify ModelMeshServing instance is in Ready phase when modelmeshserving deployments are up and running.
func (tc *ModelMeshServingTestCtx) validateModelMeshServingReady() error {
	err := wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testModelMeshServingInstance.Name}
		mm := &componentApi.ModelMeshServing{}

		err := tc.testCtx.customClient.Get(ctx, key, mm)
		if err != nil {
			return false, err
		}
		return mm.Status.Phase == readyStatus, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting Ready state for ModelMeshServing %v: %w", tc.testModelMeshServingInstance.Name, err)
	}

	return nil
}

func (tc *ModelMeshServingTestCtx) testUpdateOnModelMeshServingResources() error {
	// Test Updating ModelMeshServing Replicas

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(componentApi.ModelMeshServingKind),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 2 { // modelmesh has 2 deployments: modelmesh and etcd
		return fmt.Errorf("error getting deployment for component by label %s", labels.PlatformPartOf+"="+strings.ToLower(componentApi.ModelMeshServingKind))
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

func (tc *ModelMeshServingTestCtx) testUpdateModelMeshServingComponentDisabled() error {
	// Test Updating ModelMeshServing to be disabled
	var mmDeploymentName string

	if tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component(componentApi.ModelMeshServingComponentName),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", componentApi.ModelMeshServingComponentName)
		}
		if len(appDeployments.Items) > 0 {
			mmDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", mmDeploymentName)
			}
		}
	} else {
		return errors.New("modelmeshserving spec should be in 'enabled: true' state in order to perform test")
	}

	// Disable component ModelMeshServing
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh DSC instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable the Component
		tc.testCtx.testDsc.Spec.Components.ModelMeshServing.ManagementState = operatorv1.Removed

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
		// Verify ModeMeshServing CR is deleted
		mm := &componentApi.ModelMeshServing{}
		err = tc.testCtx.customClient.Get(ctx, client.ObjectKey{Name: tc.testModelMeshServingInstance.Name}, mm)
		return k8serr.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("component modemeshserving is disabled, should not get the ModelMeshServing CR %v", tc.testModelMeshServingInstance.Name)
	}

	// Sleep for 20 seconds to allow the operator to reconcile
	time.Sleep(2 * generalRetryInterval)
	_, err = tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).Get(tc.testCtx.ctx, mmDeploymentName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		componentApi.ModelMeshServingKind,
		mmDeploymentName,
		tc.testCtx.applicationsNamespace)
}
