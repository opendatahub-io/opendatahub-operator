package e2e_test

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
)

const (
	odhLabelPrefix = "app.opendatahub.io/"
)

func creationTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)
	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Creation of DSCI CR", func(t *testing.T) {
			err = testCtx.testDSCICreation()
			require.NoError(t, err, "error creating DSCI CR")
		})
		t.Run("Creation of more than one of DSCInitialization instance", func(t *testing.T) {
			testCtx.testDSCIDuplication(t)
		})
		t.Run("Creation of DataScienceCluster instance", func(t *testing.T) {
			err = testCtx.testDSCCreation()
			require.NoError(t, err, "error creating DataScienceCluster instance")
		})
		t.Run("Creation of more than one of DataScienceCluster instance", func(t *testing.T) {
			testCtx.testDSCDuplication(t)
		})
		t.Run("Validate all deployed components", func(t *testing.T) {
			err = testCtx.testAllApplicationCreation(t)
			require.NoError(t, err, "error testing deployments for DataScienceCluster: "+testCtx.testDsc.Name)
		})
		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = testCtx.testOwnerrefrences()
			require.NoError(t, err, "error getting all DataScienceCluster's Ownerrefrences")
		})
		t.Run("Validate Controller reconcile", func(t *testing.T) {
			// only test Dashboard component for now
			err = testCtx.testUpdateComponentReconcile()
			require.NoError(t, err, "error testing updates for DSC managed resource")
		})
		t.Run("Validate Component Enabled field", func(t *testing.T) {
			err = testCtx.testUpdateDSCComponentEnabled()
			require.NoError(t, err, "error testing component enabled field")
		})
	})
}

func (tc *testContext) testDSCICreation() error {
	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	createdDSCI := &dsci.DSCInitialization{}
	existingDSCIList := &dsci.DSCInitializationList{}

	err := tc.customClient.List(tc.ctx, existingDSCIList)
	if err == nil {
		// use what you have
		if len(existingDSCIList.Items) == 1 {
			tc.testDSCI = &existingDSCIList.Items[0]
			return nil
		}
	}
	// create one for you
	err = tc.customClient.Get(tc.ctx, dscLookupKey, createdDSCI)
	if err != nil {
		if errors.IsNotFound(err) {
			nberr := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (bool, error) {
				creationErr := tc.customClient.Create(tc.ctx, tc.testDSCI)
				if creationErr != nil {
					log.Printf("error creating DSCI resource %v: %v, trying again",
						tc.testDSCI.Name, creationErr)
					return false, nil
				}
				return true, nil
			})
			if nberr != nil {
				return fmt.Errorf("error creating e2e-test-dsci DSCI CR %s: %w", tc.testDSCI.Name, nberr)
			}
		} else {
			return fmt.Errorf("error getting e2e-test-dsci DSCI CR %s: %w", tc.testDSCI.Name, err)
		}
	}

	return nil
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
			nberr := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (bool, error) {
				creationErr := tc.customClient.Create(tc.ctx, tc.testDsc)
				if creationErr != nil {
					log.Printf("error creating DSC resource %v: %v, trying again",
						tc.testDsc.Name, creationErr)

					return false, nil
				}
				return true, nil
			})
			if nberr != nil {
				return fmt.Errorf("error creating e2e-test DSC %s: %w", tc.testDsc.Name, nberr)
			}
		} else {
			return fmt.Errorf("error getting e2e-test DSC %s: %w", tc.testDsc.Name, err)
		}
	}

	return nil
}

func (tc *testContext) requireInstalled(t *testing.T, gvk schema.GroupVersionKind) {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	err := tc.customClient.List(tc.ctx, list)
	require.NoErrorf(t, err, "Could not get %s list", gvk.Kind)

	require.Greaterf(t, len(list.Items), 0, "%s has not been installed", gvk.Kind)
}

func (tc *testContext) testDuplication(t *testing.T, gvk schema.GroupVersionKind, o any) {
	t.Helper()
	tc.requireInstalled(t, gvk)

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	require.NoErrorf(t, err, "Could not unstructure %s", gvk.Kind)

	obj := &unstructured.Unstructured{
		Object: u,
	}
	obj.SetGroupVersionKind(gvk)

	err = tc.customClient.Create(tc.ctx, obj)

	require.Errorf(t, err, "Could create second %s", gvk.Kind)
}

func (tc *testContext) testDSCIDuplication(t *testing.T) { //nolint:thelper
	gvk := schema.GroupVersionKind{
		Group:   "dscinitialization.opendatahub.io",
		Version: "v1",
		Kind:    "DSCInitialization",
	}
	dup := setupDSCICR("e2e-test-dsci-dup")

	tc.testDuplication(t, gvk, dup)
}

func (tc *testContext) testDSCDuplication(t *testing.T) { //nolint:thelper
	gvk := schema.GroupVersionKind{
		Group:   "datasciencecluster.opendatahub.io",
		Version: "v1",
		Kind:    "DataScienceCluster",
	}
	dup := setupDSCInstance("e2e-test-dup")

	tc.testDuplication(t, gvk, dup)
}

func (tc *testContext) testAllApplicationCreation(t *testing.T) error { //nolint:funlen,thelper
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

	t.Run("Validate Dashboard", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.Dashboard))
		if tc.testDsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.Dashboard.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.Dashboard.GetComponentName())
		}
	})

	t.Run("Validate ModelMeshServing", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.ModelMeshServing))
		if tc.testDsc.Spec.Components.ModelMeshServing.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.ModelMeshServing.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.ModelMeshServing.GetComponentName())
		}
	})

	t.Run("Validate Kserve", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.Kserve))
		// test Unmanaged state, since servicemesh is not installed.
		if tc.testDsc.Spec.Components.Kserve.ManagementState == operatorv1.Managed {
			if err != nil && tc.testDsc.Spec.Components.Kserve.Serving.ManagementState == operatorv1.Unmanaged {
				require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.Workbenches.GetComponentName())
			} else {
				require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.Kserve.GetComponentName())
			}
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.Kserve.GetComponentName())
		}
	})

	t.Run("Validate Workbenches", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.Workbenches))
		if tc.testDsc.Spec.Components.Workbenches.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.Workbenches.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.Workbenches.GetComponentName())
		}
	})

	t.Run("Validate DataSciencePipelines", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.DataSciencePipelines))
		if tc.testDsc.Spec.Components.DataSciencePipelines.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.DataSciencePipelines.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.DataSciencePipelines.GetComponentName())
		}
	})

	t.Run("Validate CodeFlare", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.CodeFlare))
		if tc.testDsc.Spec.Components.CodeFlare.ManagementState == operatorv1.Managed {
			// dependent operator error, as expected
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.CodeFlare.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.CodeFlare.GetComponentName())
		}
	})

	t.Run("Validate Ray", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.Ray))
		if tc.testDsc.Spec.Components.Ray.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.Ray.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.Ray.GetComponentName())
		}
	})

	t.Run("Validate Kueue", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.Kueue))
		if tc.testDsc.Spec.Components.Kueue.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.Kueue.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.Kueue.GetComponentName())
		}
	})

	t.Run("Validate TrustyAI", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.TrustyAI))
		if tc.testDsc.Spec.Components.TrustyAI.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.TrustyAI.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.TrustyAI.GetComponentName())
		}
	})

	t.Run("Validate ModelRegistry", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.ModelRegistry))
		if tc.testDsc.Spec.Components.ModelRegistry.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.ModelRegistry.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.ModelRegistry.GetComponentName())
		}
	})
	t.Run("Validate TrainingOperator", func(t *testing.T) {
		// speed testing in parallel
		t.Parallel()
		err = tc.testApplicationCreation(&(tc.testDsc.Spec.Components.TrainingOperator))
		if tc.testDsc.Spec.Components.TrainingOperator.ManagementState == operatorv1.Managed {
			require.NoError(t, err, "error validating application %v when enabled", tc.testDsc.Spec.Components.TrainingOperator.GetComponentName())
		} else {
			require.Error(t, err, "error validating application %v when disabled", tc.testDsc.Spec.Components.TrainingOperator.GetComponentName())
		}
	})
	return nil
}

func (tc *testContext) testApplicationCreation(component components.ComponentInterface) error {
	err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (bool, error) {
		// TODO: see if checking deployment is a good test, CF does not create deployment
		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: odhLabelPrefix + component.GetComponentName(),
		})
		if err != nil {
			log.Printf("error listing application deployments :%v. Trying again...", err)

			return false, fmt.Errorf("error listing application deployments :%w. Trying again", err)
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
			}
			log.Printf("waiting for application deployments to be in Ready state.")
			return false, nil
		}
		// when no deployment is found
		// check Reconcile failed with missing dependent operator error
		for _, Condition := range tc.testDsc.Status.Conditions {
			if strings.Contains(Condition.Message, "Please install the operator before enabling "+component.GetComponentName()) {
				return true, err
			}
		}
		return false, nil
	})

	return err
}

func (tc *testContext) testOwnerrefrences() error {
	// Test any one of the apps
	if tc.testDsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: odhLabelPrefix + tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
		})
		if err != nil {
			return fmt.Errorf("error listing application deployments %w", err)
		}
		// test any one deployment for ownerreference
		if len(appDeployments.Items) != 0 && appDeployments.Items[0].OwnerReferences[0].Kind != "DataScienceCluster" {
			return fmt.Errorf("expected ownerreference not found. Got ownereferrence: %v",
				appDeployments.Items[0].OwnerReferences)
		}
	}

	return nil
}

func (tc *testContext) testUpdateComponentReconcile() error {
	// Test Updating Dashboard Replicas

	appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: odhLabelPrefix + tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
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
			return fmt.Errorf("error patching component resources : %w", err)
		}
		if retrievedDep.Spec.Replicas != patchedReplica.Spec.Replicas {
			return fmt.Errorf("failed to patch replicas : expect to be %v but got %v", patchedReplica.Spec.Replicas, retrievedDep.Spec.Replicas)
		}

		// Sleep for 40 seconds to allow the operator to reconcile
		time.Sleep(4 * tc.resourceRetryInterval)
		revertedDep, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).Get(context.TODO(), testDeployment.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error getting component resource after reconcile: %w", err)
		}
		if *revertedDep.Spec.Replicas != *expectedReplica {
			return fmt.Errorf("failed to revert back replicas : expect to be %v but got %v", *expectedReplica, *revertedDep.Spec.Replicas)
		}
	}

	return nil
}

func (tc *testContext) testUpdateDSCComponentEnabled() error {
	// Test Updating dashboard to be disabled
	var dashboardDeploymentName string

	if tc.testDsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: odhLabelPrefix + tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", tc.testDsc.Spec.Components.Dashboard.GetComponentName())
		}
		if len(appDeployments.Items) > 0 {
			dashboardDeploymentName = appDeployments.Items[0].Name
			if appDeployments.Items[0].Status.ReadyReplicas == 0 {
				return fmt.Errorf("error getting enabled component: %s its deployment 'ReadyReplicas'", dashboardDeploymentName)
			}
		}
	} else {
		return fmt.Errorf("dashboard spec should be in 'enabled: true' state in order to perform test")
	}

	// Disable component Dashboard
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh the instance in case it was updated during the reconcile
		err := tc.customClient.Get(tc.ctx, types.NamespacedName{Name: tc.testDsc.Name}, tc.testDsc)
		if err != nil {
			return fmt.Errorf("error getting resource %w", err)
		}
		// Disable the Component
		tc.testDsc.Spec.Components.Dashboard.ManagementState = operatorv1.Removed

		// Try to update
		err = tc.customClient.Update(context.TODO(), tc.testDsc)
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

	// Sleep for 40 seconds to allow the operator to reconcile
	time.Sleep(4 * tc.resourceRetryInterval)
	_, err = tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).Get(context.TODO(), dashboardDeploymentName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}

		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
		dashboardDeploymentName,
		tc.applicationsNamespace)
}
