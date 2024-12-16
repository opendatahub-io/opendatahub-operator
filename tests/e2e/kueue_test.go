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
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

type KueueTestCtx struct {
	testCtx           *testContext
	testKueueInstance componentApi.Kueue
}

func kueueTestSuite(t *testing.T) {
	t.Helper()

	kueueCtx := KueueTestCtx{}
	var err error
	kueueCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := kueueCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// creation
		t.Run("Creation of Kueue CR", func(t *testing.T) {
			err = kueueCtx.testKueueCreation()
			require.NoError(t, err, "error creating Kueue CR")
		})

		t.Run("Validate Kueue instance", func(t *testing.T) {
			err = kueueCtx.validateKueue()
			require.NoError(t, err, "error validating Kueue instance")
		})

		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = kueueCtx.testOwnerReferences()
			require.NoError(t, err, "error getting all Kueue's Ownerrefrences")
		})

		t.Run("Validate Kueue Ready", func(t *testing.T) {
			err = kueueCtx.validateKueueReady()
			require.NoError(t, err, "Kueue instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			err = kueueCtx.testUpdateOnKueueResources()
			require.NoError(t, err, "error testing updates for Kueue's managed resources")
		})

		t.Run("Validate Disabling Kueue Component", func(t *testing.T) {
			err = kueueCtx.testUpdateKueueComponentDisabled()
			require.NoError(t, err, "error testing kueue component enabled field")
		})
	})
}

func (tc *KueueTestCtx) testKueueCreation() error {
	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		key := client.ObjectKeyFromObject(tc.testCtx.testDsc)

		err := tc.testCtx.customClient.Get(ctx, key, tc.testCtx.testDsc)
		if err != nil {
			return false, fmt.Errorf("error getting resource %w", err)
		}

		tc.testCtx.testDsc.Spec.Components.Kueue.ManagementState = operatorv1.Managed

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
		existingKueueList := &componentApi.KueueList{}

		if err := tc.testCtx.customClient.List(ctx, existingKueueList); err != nil {
			return false, err
		}

		switch {
		case len(existingKueueList.Items) == 1:
			tc.testKueueInstance = existingKueueList.Items[0]
			return true, nil
		case len(existingKueueList.Items) > 1:
			return false, fmt.Errorf(
				"unexpected Kueue CR instances. Expected 1 , Found %v instance", len(existingKueueList.Items))
		default:
			return false, nil
		}
	})

	if err != nil {
		return fmt.Errorf("unable to find Kueue CR instance: %w", err)
	}

	return nil
}

func (tc *KueueTestCtx) validateKueue() error {
	// Kueue spec should match the spec of Kueue component in DSC
	if !reflect.DeepEqual(tc.testCtx.testDsc.Spec.Components.Kueue.KueueCommonSpec, tc.testKueueInstance.Spec.KueueCommonSpec) {
		err := fmt.Errorf("expected .spec for Kueue %v, got %v",
			tc.testCtx.testDsc.Spec.Components.Kueue.KueueCommonSpec, tc.testKueueInstance.Spec.KueueCommonSpec)
		return err
	}
	return nil
}

func (tc *KueueTestCtx) testOwnerReferences() error {
	if len(tc.testKueueInstance.OwnerReferences) != 1 {
		return errors.New("expect CR has ownerreferences set")
	}

	// Test Kueue CR ownerref
	if tc.testKueueInstance.OwnerReferences[0].Kind != dscKind {
		return fmt.Errorf("expected ownerreference DataScienceCluster not found. Got ownereferrence: %v",
			tc.testKueueInstance.OwnerReferences[0].Kind)
	}

	// Test Kueue resources
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.Kueue.Kind),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}
	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != componentApi.KueueKind {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify Kueue instance is in Ready phase when kueue deployments are up and running.
func (tc *KueueTestCtx) validateKueueReady() error {
	err := wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testKueueInstance.Name}
		kueue := &componentApi.Kueue{}

		err := tc.testCtx.customClient.Get(ctx, key, kueue)
		if err != nil {
			return false, err
		}
		return kueue.Status.Phase == readyStatus, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting on Ready state for Kueue %v: %w", tc.testKueueInstance.Name, err)
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
			if c.Type == componentApi.KueueComponentName+"Ready" {
				return c.Status == corev1.ConditionTrue, nil
			}
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting on Ready state for Kueue component in DSC: %w", err)
	}

	return nil
}

func (tc *KueueTestCtx) testUpdateOnKueueResources() error {
	// Test Updating Kueue Replicas

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(tc.testKueueInstance.Kind),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 1 {
		return fmt.Errorf("error getting deployment for component %s", tc.testKueueInstance.Name)
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

func (tc *KueueTestCtx) testUpdateKueueComponentDisabled() error {
	// Test Updating Kueue to be disabled
	var kueueDeploymentName string

	if tc.testCtx.testDsc.Spec.Components.Kueue.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
			LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.Kueue.Kind),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", componentApi.KueueComponentName)
		}
		if len(appDeployments.Items) > 0 {
			kueueDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", kueueDeploymentName)
			}
		}
	} else {
		return errors.New("kueue spec should be in 'enabled: true' state in order to perform test")
	}

	// Disable component Kueue
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh DSC instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable the Component
		tc.testCtx.testDsc.Spec.Components.Kueue.ManagementState = operatorv1.Removed

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
		// Verify kueue CR is deleted
		kueue := &componentApi.Kueue{}
		err = tc.testCtx.customClient.Get(ctx, client.ObjectKey{Name: tc.testKueueInstance.Name}, kueue)
		return k8serr.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("component kueue is disabled, should not get the Kueue CR %v", tc.testKueueInstance.Name)
	}

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.Kueue.Kind),
	})

	if err != nil {
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	if len(appDeployments.Items) != 0 {
		return fmt.Errorf("component %v is disabled, should not have deployments in namespace %v any more",
			componentApi.KueueKind,
			tc.testCtx.applicationsNamespace)
	}

	return nil
}
