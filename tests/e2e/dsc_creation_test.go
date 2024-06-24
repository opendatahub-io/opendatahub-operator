package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
)

const (
	odhLabelPrefix = "app.opendatahub.io/"
)

func creationTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	err = testCtx.setUp(t)
	require.NoError(t, err, "error setting up environment")

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
		t.Run("Validate DSCInitialization instance", func(t *testing.T) {
			err = testCtx.validateDSCI()
			require.NoError(t, err, "error validating DSCInitialization instance")
		})
		t.Run("Validate DataScienceCluster instance", func(t *testing.T) {
			err = testCtx.validateDSC()
			require.NoError(t, err, "error validating DataScienceCluster instance")
		})
		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = testCtx.testOwnerrefrences()
			require.NoError(t, err, "error getting all DataScienceCluster's Ownerrefrences")
		})
		t.Run("Validate default certs available", func(t *testing.T) {
			err = testCtx.testDefaultCertsAvailable()
			require.NoError(t, err, "error getting default cert secrets for Kserve")
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
		if k8serrors.IsNotFound(err) {
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

func waitDSCReady(tc *testContext) error {
	err := tc.wait(func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testDsc.Name}
		dsc := &dsc.DataScienceCluster{}

		err := tc.customClient.Get(tc.ctx, key, dsc)
		if err != nil {
			return false, err
		}

		return dsc.Status.Phase == "Ready", nil
	})

	if err != nil {
		return fmt.Errorf("Error waiting Ready state for DSC %v: %w", tc.testDsc.Name, err)
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
		if k8serrors.IsNotFound(err) {
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

	return waitDSCReady(tc)
}

func (tc *testContext) requireInstalled(t *testing.T, gvk schema.GroupVersionKind) {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	err := tc.customClient.List(tc.ctx, list)
	require.NoErrorf(t, err, "Could not get %s list", gvk.Kind)

	require.NotEmptyf(t, len(list.Items), "%s has not been installed", gvk.Kind)
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

	components, err := tc.testDsc.GetComponents()
	if err != nil {
		return err
	}

	for _, c := range components {
		c := c
		name := c.GetComponentName()
		t.Run("Validate "+name, func(t *testing.T) {
			t.Parallel()

			err = tc.testApplicationCreation(c)

			msg := fmt.Sprintf("error validating application %v when ", name)
			if c.GetManagementState() == operatorv1.Managed {
				require.NoError(t, err, msg+"enabled")
			} else {
				require.Error(t, err, msg+"disabled")
			}
		})
	}

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

func (tc *testContext) validateDSCI() error {
	// expected
	expServiceMeshSpec := &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
		ControlPlane: infrav1.ControlPlaneSpec{
			Name:              "data-science-smcp",
			Namespace:         "istio-system",
			MetricsCollection: "Istio",
		},
		Auth: infrav1.AuthSpec{
			Audiences: &[]string{"https://kubernetes.default.svc"},
		},
	}

	// actual
	act := tc.testDSCI

	if !reflect.DeepEqual(act.Spec.ServiceMesh, expServiceMeshSpec) {
		err := fmt.Errorf("Expected service mesh spec %v, got %v",
			expServiceMeshSpec, act.Spec.ServiceMesh)
		return err
	}

	return nil
}

func (tc *testContext) validateDSC() error {
	expServingSpec := infrav1.ServingSpec{
		ManagementState: operatorv1.Managed,
		Name:            "knative-serving",
		IngressGateway: infrav1.IngressGatewaySpec{
			Certificate: infrav1.CertificateSpec{
				Type: infrav1.OpenshiftDefaultIngress,
			},
		},
	}

	act := tc.testDsc

	if act.Spec.Components.Kserve.Serving != expServingSpec {
		err := fmt.Errorf("Expected serving spec %v, got %v",
			expServingSpec, act.Spec.Components.Kserve.Serving)
		return err
	}

	return nil
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

func (tc *testContext) testDefaultCertsAvailable() error {
	// Get expected cert secrets
	defaultIngressCtrl, err := cluster.FindAvailableIngressController(tc.ctx, tc.customClient)
	if err != nil {
		return fmt.Errorf("failed to get ingress controller: %w", err)
	}

	defaultIngressCertName := cluster.GetDefaultIngressCertSecretName(defaultIngressCtrl)

	defaultIngressSecret, err := cluster.GetSecret(tc.ctx, tc.customClient, "openshift-ingress", defaultIngressCertName)
	if err != nil {
		return err
	}

	// Verify secret from Control Plane namespace matches the default cert secret
	defaultSecretName := tc.testDsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.SecretName
	if defaultSecretName == "" {
		defaultSecretName = serverless.DefaultCertificateSecretName
	}
	ctrlPlaneSecret, err := cluster.GetSecret(tc.ctx, tc.customClient, tc.testDSCI.Spec.ServiceMesh.ControlPlane.Namespace,
		defaultSecretName)
	if err != nil {
		return err
	}
	if string(defaultIngressSecret.Data["tls.crt"]) != string(ctrlPlaneSecret.Data["tls.crt"]) {
		return fmt.Errorf("default cert secret not expected. Epected %v, Got %v", defaultIngressSecret.Data["tls.crt"], ctrlPlaneSecret.Data["tls.crt"])
	}

	if string(defaultIngressSecret.Data["tls.key"]) != string(ctrlPlaneSecret.Data["tls.key"]) {
		return fmt.Errorf("default cert secret not expected. Epected %v, Got %v", defaultIngressSecret.Data["tls.crt"], ctrlPlaneSecret.Data["tls.crt"])
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
		return errors.New("dashboard spec should be in 'enabled: true' state in order to perform test")
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
		if k8serrors.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}

		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
		dashboardDeploymentName,
		tc.applicationsNamespace)
}
