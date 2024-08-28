package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func creationTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	err = testCtx.setUp(t)
	require.NoError(t, err, "error setting up environment")

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		// DSCI
		t.Run("Creation of DSCI CR", func(t *testing.T) {
			err = testCtx.testDSCICreation()
			require.NoError(t, err, "error creating DSCI CR")
		})

		t.Run("Creation of more than one of DSCInitialization instance", func(t *testing.T) {
			testCtx.testDSCIDuplication(t)
		})

		t.Run("Validate DSCInitialization instance", func(t *testing.T) {
			err = testCtx.validateDSCI()
			require.NoError(t, err, "error validating DSCInitialization instance")

		})
		t.Run("Creation of DataScienceCluster instance", func(t *testing.T) {
			err = testCtx.testDSCCreation()
			require.NoError(t, err, "error creating DataScienceCluster instance")
		})
		t.Run("Creation of more than one of DataScienceCluster instance", func(t *testing.T) {
			testCtx.testDSCDuplication(t)
		})

		t.Run("Validate DSCInitialization instance", func(t *testing.T) {
			err = testCtx.validateDSCI()
			require.NoError(t, err, "error validating DSCInitialization instance")
		})

		t.Run("Validate Ownerrefrences exist", func(t *testing.T) {
			err = testCtx.testOwnerrefrences()
			require.NoError(t, err, "error getting all DataScienceCluster's Ownerrefrences")
		})
		t.Run("Validate all deployed components", func(t *testing.T) {
			// this will take about 5-6 mins to complete
			err = testCtx.testAllComponentCreation(t)
			require.NoError(t, err, "error testing deployments for DataScienceCluster: "+testCtx.testDsc.Name)
		})
		t.Run("Validate DSC Ready", func(t *testing.T) {
			err = testCtx.validateDSCReady()
			require.NoError(t, err, "DataScienceCluster instance is not Ready")
		})

		// Kserve
		t.Run("Validate Knative resoruce", func(t *testing.T) {
			err = testCtx.validateDSC()
			require.NoError(t, err, "error getting Knatvie resrouce as part of DataScienceCluster validation")
		})
		t.Run("Validate default certs available", func(t *testing.T) {
			// move it to be part of check with kserve since it is using serving's secret
			err = testCtx.testDefaultCertsAvailable()
			require.NoError(t, err, "error getting default cert secrets for Kserve")
		})

		// TODO: enable when ModelReg is added
		// t.Run("Validate default model registry cert available", func(t *testing.T) {
		// 	err = testCtx.testDefaultModelRegistryCertAvailable()
		// 	require.NoError(t, err, "error getting default cert secret for ModelRegistry")
		// })
		// t.Run("Validate model registry servicemeshmember available", func(t *testing.T) {
		// 	err = testCtx.testMRServiceMeshMember()
		// 	require.NoError(t, err, "error getting servicemeshmember for Model Registry")
		// })

		// reconcile
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
	createdDSCI := &dsciv1.DSCInitialization{}
	existingDSCIList := &dsciv1.DSCInitializationList{}

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
		if k8serr.IsNotFound(err) {
			nberr := wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, dsciCreationTimeout, false, func(ctx context.Context) (bool, error) {
				creationErr := tc.customClient.Create(ctx, tc.testDSCI)
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
	createdDSC := &dscv1.DataScienceCluster{}
	existingDSCList := &dscv1.DataScienceClusterList{}

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
		if k8serr.IsNotFound(err) {
			dsciErr := wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, dscCreationTimeout, false, func(ctx context.Context) (bool, error) {
				creationErr := tc.customClient.Create(ctx, tc.testDsc)
				if creationErr != nil {
					log.Printf("error creating DSC resource %v: %v, trying again",
						tc.testDsc.Name, creationErr)
					return false, nil
				}
				return true, nil
			})
			if dsciErr != nil {
				return fmt.Errorf("error creating e2e-test-dsc DSC %s: %w", tc.testDsc.Name, dsciErr)
			}
		} else {
			return fmt.Errorf("error getting e2e-test-dsc DSC %s: %w", tc.testDsc.Name, err)
		}
	}
	return nil
}

func (tc *testContext) validateDSCReady() error {
	return waitDSCReady(tc)
}

func waitDSCReady(tc *testContext) error {
	// wait for 2 mins which is on the safe side, normally it should get ready once all components are ready
	err := tc.wait(func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testDsc.Name}
		dsc := &dscv1.DataScienceCluster{}

		err := tc.customClient.Get(ctx, key, dsc)
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

func (tc *testContext) requireInstalled(t *testing.T, gvk schema.GroupVersionKind) {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	err := tc.customClient.List(tc.ctx, list)
	require.NotEmptyf(t, err, "Could not get %s list", gvk.Kind)
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
	dup := setupDSCInstance("e2e-test-dsc-dup")

	tc.testDuplication(t, gvk, dup)
}

func (tc *testContext) testAllComponentCreation(t *testing.T) error { //nolint:funlen,thelper
	// Validate all components are in Ready state

	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	createdDSC := &dscv1.DataScienceCluster{}

	// Wait for components to get deployed
	time.Sleep(1 * time.Minute)

	err := tc.customClient.Get(tc.ctx, dscLookupKey, createdDSC)
	if err != nil {
		return fmt.Errorf("error getting DataScienceCluster instance :%v", tc.testDsc.Name)
	}
	tc.testDsc = createdDSC

	components, err := tc.testDsc.GetComponents()
	if err != nil {
		return err
	}

	for _, c := range components {
		c := c
		name := c.GetComponentName()
		t.Run("Validate "+name, func(t *testing.T) {
			t.Parallel()
			err = tc.testComponentCreation(c)
			require.NoError(t, err, "error validating component %v when "+c.GetManagementState())
		})
	}

	// Verify DSC instance is in Ready phase in the end when all components are up and running
	if tc.testDsc.Status.Phase != "Ready" {
		return fmt.Errorf("DSC instance is not in Ready phase. Current phase: %v", tc.testDsc.Status.Phase)
	}

	return nil
}

func (tc *testContext) testComponentCreation(component components.ComponentInterface) error {
	err := wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		// TODO: see if checking deployment is a good test, CF does not create deployment
		var componentName = component.GetComponentName()
		if component.GetComponentName() == "dashboard" { // special case for RHOAI dashboard name
			componentName = "rhods-dashboard"
		}

		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component(componentName),
		})
		if err != nil {
			log.Printf("error listing component deployments :%v", err)
			return false, fmt.Errorf("error listing component deployments :%w", err)
		}
		if len(appList.Items) != 0 {
			if component.GetManagementState() == operatorv1.Removed {
				// deployment exists for removed component, retrying
				return false, nil
			}

			for _, deployment := range appList.Items {
				if deployment.Status.ReadyReplicas < 1 {
					log.Printf("waiting for component deployments to be in Ready state: %s", deployment.Name)
					return false, nil
				}
			}
			return true, nil
		}
		// when no deployment is found
		// It's ok not to have deployements for unmanaged component
		if component.GetManagementState() != operatorv1.Managed {
			return true, nil
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

// test if knative resource has been created.
func (tc *testContext) validateDSC() error {
	expServingSpec := infrav1.ServingSpec{
		ManagementState: operatorv1.Managed,
		Name:            "knative-serving",
		IngressGateway: infrav1.GatewaySpec{
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
	// Test Dashboard component
	if tc.testDsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(tc.ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component("rhods-dashboard"),
		})
		if err != nil {
			return fmt.Errorf("error listing component deployments %w", err)
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

	if ctrlPlaneSecret.Type != defaultIngressSecret.Type {
		return fmt.Errorf("wrong type of cert secret is created for %v. Expected %v, Got %v", defaultSecretName, defaultIngressSecret.Type, ctrlPlaneSecret.Type)
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
	appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(tc.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.Component("rhods-dashboard"),
	})
	if err != nil {
		return err
	}

	if len(appDeployments.Items) != 1 {
		return fmt.Errorf("error getting deployment for component %s", tc.testDsc.Spec.Components.Dashboard.GetComponentName())
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
	updatedDep, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).UpdateScale(tc.ctx, testDeployment.Name, patchedReplica, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error patching component resources : %w", err)
	}
	if updatedDep.Spec.Replicas != patchedReplica.Spec.Replicas {
		return fmt.Errorf("failed to patch replicas : expect to be %v but got %v", patchedReplica.Spec.Replicas, updatedDep.Spec.Replicas)
	}

	// Sleep for 40 seconds to allow the operator to reconcile
	// we expect it should not revert back to original value because of AllowList
	time.Sleep(4 * generalRetryInterval)
	reconciledDep, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).Get(tc.ctx, testDeployment.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	if *reconciledDep.Spec.Replicas != expectedReplica {
		return fmt.Errorf("failed to revert back replicas : expect to be %v but got %v", expectedReplica, *reconciledDep.Spec.Replicas)
	}

	return nil
}

func (tc *testContext) testUpdateDSCComponentEnabled() error {
	// Test Updating dashboard to be disabled
	var dashboardDeploymentName string

	if tc.testDsc.Spec.Components.Dashboard.ManagementState == operatorv1.Managed {
		appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(tc.ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.Component("rhods-dashboard"),
		})
		if err != nil {
			return fmt.Errorf("error getting enabled component %v", "rhods-dashboard")
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
		err = tc.customClient.Update(tc.ctx, tc.testDsc)
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

	// Sleep for 80 seconds to allow the operator to reconcile
	time.Sleep(8 * generalRetryInterval)
	_, err = tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).Get(tc.ctx, dashboardDeploymentName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil // correct result: should not find deployment after we disable it already
		}
		return fmt.Errorf("error getting component resource after reconcile: %w", err)
	}
	return fmt.Errorf("component %v is disabled, should not get its deployment %v from NS %v any more",
		tc.testDsc.Spec.Components.Dashboard.GetComponentName(),
		dashboardDeploymentName,
		tc.applicationsNamespace)
}
