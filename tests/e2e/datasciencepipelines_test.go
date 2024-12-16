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

type DataSciencePipelinesTestCtx struct {
	testCtx                          *testContext
	testDataSciencePipelinesInstance componentApi.DataSciencePipelines
}

func dataSciencePipelinesTestSuite(t *testing.T) {
	t.Helper()

	dspCtx := DataSciencePipelinesTestCtx{}
	var err error
	dspCtx.testCtx, err = NewTestContext()
	require.NoError(t, err)

	testCtx := dspCtx.testCtx

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// creation
		t.Run("Creation of DataSciencePipelines CR", func(t *testing.T) {
			err = dspCtx.testDSPCreation()
			require.NoError(t, err, "error creating DataSciencePipelines CR")
		})

		t.Run("Validate DataSciencePipelines instance", func(t *testing.T) {
			err = dspCtx.validateDataSciencePipelines()
			require.NoError(t, err, "error validating DataSciencePipelines instance")
		})

		t.Run("Validate Ownerreferences exist", func(t *testing.T) {
			err = dspCtx.testOwnerReferences()
			require.NoError(t, err, "error getting all DataSciencePipelines' Ownerreferences")
		})

		t.Run("Validate get and list of DataSciencePipelienes instance", func(t *testing.T) {
			err = dspCtx.getAndListDataSciencePipelines()
			require.NoError(t, err, "error getting and listing DataSciencePipelines instance")
		})

		t.Run("Validate DataSciencePipelines Ready", func(t *testing.T) {
			err = dspCtx.validateDataSciencePipelinesReady()
			require.NoError(t, err, "DataSciencePipelines instance is not Ready")
		})

		// reconcile
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			err = dspCtx.testUpdateOnDataSciencePipelinesResources()
			require.NoError(t, err, "error testing updates for DataSciencePipeline's managed resources")
		})

		// disable
		t.Run("Validate Disabling DataSciencePipelines Component", func(t *testing.T) {
			err = dspCtx.testUpdateDataSciencePipelinesComponentDisabled()
			require.NoError(t, err, "error testing DataSciencePipelines component enabled field")
		})
	})
}

func (tc *DataSciencePipelinesTestCtx) testDSPCreation() error {
	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		key := client.ObjectKeyFromObject(tc.testCtx.testDsc)

		err := tc.testCtx.customClient.Get(ctx, key, tc.testCtx.testDsc)
		if err != nil {
			return false, fmt.Errorf("error getting resource %w", err)
		}

		tc.testCtx.testDsc.Spec.Components.DataSciencePipelines.ManagementState = operatorv1.Managed

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
		existingDSPList := &componentApi.DataSciencePipelinesList{}

		if err := tc.testCtx.customClient.List(ctx, existingDSPList); err != nil {
			return false, err
		}

		switch {
		case len(existingDSPList.Items) == 1:
			tc.testDataSciencePipelinesInstance = existingDSPList.Items[0]
			return true, nil
		case len(existingDSPList.Items) > 1:
			return false, fmt.Errorf(
				"unexpected DataSciencePipelines CR instances. Expected 1 , Found %v instance", len(existingDSPList.Items))
		default:
			return false, nil
		}
	})

	if err != nil {
		return fmt.Errorf("unable to find DataSciencePipelines CR instance: %w", err)
	}

	return nil
}

func (tc *DataSciencePipelinesTestCtx) validateDataSciencePipelines() error {
	// DataSciencePipeline spec should match the spec of DSP component in DSC
	if !reflect.DeepEqual(
		tc.testCtx.testDsc.Spec.Components.DataSciencePipelines.DataSciencePipelinesCommonSpec,
		tc.testDataSciencePipelinesInstance.Spec.DataSciencePipelinesCommonSpec) {
		err := fmt.Errorf("expected .spec for DataSciencePipelines %v, got %v",
			tc.testCtx.testDsc.Spec.Components.DataSciencePipelines.DataSciencePipelinesCommonSpec, tc.testDataSciencePipelinesInstance.Spec.DataSciencePipelinesCommonSpec)
		return err
	}
	return nil
}

func (tc *DataSciencePipelinesTestCtx) getAndListDataSciencePipelines() error {
	err := tc.testCtx.wait(func(ctx context.Context) (bool, error) {
		dspList := &componentApi.DataSciencePipelinesList{}

		if err := tc.testCtx.customClient.List(ctx, dspList); err != nil {
			return false, err
		}

		if len(dspList.Items) == 0 {
			return false, errors.New("no DataSciencePipelines CR instances found when trying to list")
		}

		key := types.NamespacedName{Name: dspList.Items[0].Name, Namespace: dspList.Items[0].Namespace}
		dspInstance := &componentApi.DataSciencePipelines{}

		if err := tc.testCtx.customClient.Get(ctx, key, dspInstance); err != nil {
			return false, err
		}

		return true, nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (tc *DataSciencePipelinesTestCtx) testOwnerReferences() error {
	if len(tc.testDataSciencePipelinesInstance.OwnerReferences) != 1 {
		return errors.New("expect CR has ownerreferences set")
	}

	// Test CR ownerref
	if tc.testDataSciencePipelinesInstance.OwnerReferences[0].Kind != "DataScienceCluster" {
		return fmt.Errorf("expected ownerreference DataScienceCluster not found. Got ownereferrence: %v",
			tc.testDataSciencePipelinesInstance.OwnerReferences[0].Kind)
	}

	// Test DataSciencePipelines resources
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.DataSciencePipelines.Kind),
	})
	if err != nil {
		return fmt.Errorf("error listing component deployments %w", err)
	}

	// test any one deployment for ownerreference
	if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != componentApi.DataSciencePipelinesKind {
		return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
			appDeployments.Items[0].OwnerReferences)
	}

	return nil
}

// Verify DataSciencePipelines instance is in Ready phase when dsp deployments are up and running.
func (tc *DataSciencePipelinesTestCtx) validateDataSciencePipelinesReady() error {
	err := wait.PollUntilContextTimeout(tc.testCtx.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testDataSciencePipelinesInstance.Name}
		dsp := &componentApi.DataSciencePipelines{}

		err := tc.testCtx.customClient.Get(ctx, key, dsp)
		if err != nil {
			return false, err
		}
		return dsp.Status.Phase == readyStatus, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting on Ready state for DataSciencePipelines %v: %w", tc.testDataSciencePipelinesInstance.Name, err)
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
			if c.Type == componentApi.DataSciencePipelinesComponentName+"Ready" {
				return c.Status == corev1.ConditionTrue, nil
			}
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("error waiting on Ready state for DataSciencePipelines component in DSC: %w", err)
	}

	return nil
}

func (tc *DataSciencePipelinesTestCtx) testUpdateOnDataSciencePipelinesResources() error {
	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.DataSciencePipelines.Kind),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 1 {
		return fmt.Errorf("error getting deployment for component %s", tc.testDataSciencePipelinesInstance.Name)
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

func (tc *DataSciencePipelinesTestCtx) testUpdateDataSciencePipelinesComponentDisabled() error {
	// Test Updating DSP to be disabled
	var dspDeploymentName string

	if tc.testCtx.testDsc.Spec.Components.DataSciencePipelines.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
			LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.DataSciencePipelines.Kind),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", componentApi.DataSciencePipelinesComponentName)
		}

		if len(appDeployments.Items) > 0 {
			dspDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", dspDeploymentName)
			}
		}
	} else {
		return errors.New("datasciencepipelines spec should be in 'enabled: true' state in order to perform test")
	}

	// Disable component DataSciencePipelines
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh DSC instance in case it was updated during the reconcile
		err := tc.testCtx.customClient.Get(tc.testCtx.ctx, types.NamespacedName{Name: tc.testCtx.testDsc.Name}, tc.testCtx.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}

		// Disable the Component
		tc.testCtx.testDsc.Spec.Components.DataSciencePipelines.ManagementState = operatorv1.Removed

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
		// Verify datasciencepipelines CR is deleted
		dsp := &componentApi.DataSciencePipelines{}
		err = tc.testCtx.customClient.Get(ctx, client.ObjectKey{Name: tc.testDataSciencePipelinesInstance.Name}, dsp)
		return k8serr.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("component datasciencepipelines is disabled, should not get the DataSciencePipelines CR %v", tc.testDataSciencePipelinesInstance.Name)
	}

	appDeployments, err := tc.testCtx.kubeClient.AppsV1().Deployments(tc.testCtx.applicationsNamespace).List(tc.testCtx.ctx, metav1.ListOptions{
		LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(gvk.DataSciencePipelines.Kind),
	})

	if err != nil {
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	if len(appDeployments.Items) != 0 {
		return fmt.Errorf("component %v is disabled, should not have deployments in namespace %v any more",
			componentApi.DataSciencePipelinesKind,
			tc.testCtx.applicationsNamespace)
	}

	return nil
}
