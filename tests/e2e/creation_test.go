//nolint:unused
package e2e_test

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
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
		if testCtx.testOpts.webhookTest {
			t.Run("Creation of more than one of DSCInitialization instance", func(t *testing.T) {
				testCtx.testDSCIDuplication(t)
			})
		}
		// Validates Servicemesh fields
		t.Run("Validate DSCInitialization instance", func(t *testing.T) {
			err = testCtx.validateDSCI()
			require.NoError(t, err, "error validating DSCInitialization instance")
		})

		t.Run("Check owned namespaces exist", testCtx.testOwnedNamespacesAllExist)

		// DSC
		t.Run("Creation of DataScienceCluster instance", func(t *testing.T) {
			err = testCtx.testDSCCreation(t)
			require.NoError(t, err, "error creating DataScienceCluster instance")
		})
		if testCtx.testOpts.webhookTest {
			t.Run("Creation of more than one of DataScienceCluster instance", func(t *testing.T) {
				testCtx.testDSCDuplication(t)
			})
		}

		// Kserve
		t.Run("Validate Knative resource", func(t *testing.T) {
			err = testCtx.validateDSC()
			require.NoError(t, err, "error getting Knative resource as part of DataScienceCluster validation")
		})

		// ModelReg
		if testCtx.testOpts.webhookTest {
			t.Run("Validate model registry config", func(t *testing.T) {
				err = testCtx.validateModelRegistryConfig()
				require.NoError(t, err, "error validating ModelRegistry config")
			})
		}
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

func (tc *testContext) testDSCCreation(t *testing.T) error {
	t.Helper()
	// Create DataScienceCluster resource if not already created

	existingDSCList := &dscv1.DataScienceClusterList{}
	err := tc.customClient.List(tc.ctx, existingDSCList)
	if err == nil {
		if len(existingDSCList.Items) > 0 {
			// Use DSC instance if it already exists
			tc.testDsc = &existingDSCList.Items[0]
			return nil
		}
	}

	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	createdDSC := &dscv1.DataScienceCluster{}
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

// Verify DSC instance is in Ready phase when all components are up and running.
func waitDSCReady(tc *testContext) error {
	// wait for 2 mins which is on the safe side, normally it should get ready once all components are ready
	err := tc.wait(func(ctx context.Context) (bool, error) {
		key := types.NamespacedName{Name: tc.testDsc.Name}
		dsc := &dscv1.DataScienceCluster{}

		err := tc.customClient.Get(ctx, key, dsc)
		if err != nil {
			return false, err
		}
		return dsc.Status.Phase == readyStatus, nil
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
	require.NoErrorf(t, err, "Could not get %s list", gvk.Kind)

	require.NotEmptyf(t, list.Items, "%s has not been installed", gvk.Kind)
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

// TODO: cleanup
// func (tc *testContext) testAllComponentCreation(t *testing.T) error { //nolint:funlen,thelper
// 	// Validate all components are in Ready state

// 	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
// 	createdDSC := &dscv1.DataScienceCluster{}

// 	// Wait for components to get deployed
// 	time.Sleep(1 * time.Minute)

// 	err := tc.customClient.Get(tc.ctx, dscLookupKey, createdDSC)
// 	if err != nil {
// 		return fmt.Errorf("error getting DataScienceCluster instance :%v", tc.testDsc.Name)
// 	}
// 	tc.testDsc = createdDSC

// 	components, err := tc.testDsc.GetComponents()
// 	if err != nil {
// 		return err
// 	}

// 	for _, c := range components {
// 		c := c
// 		name := c.GetComponentName()
// 		t.Run("Validate "+name, func(t *testing.T) {
// 			t.Parallel()
// 			err = tc.testComponentCreation(c)
// 			require.NoError(t, err, "error validating component %s when %v", name, c.GetManagementState())
// 		})
// 	}
// 	return nil
// }

// TODO: cleanup
// func (tc *testContext) testComponentCreation(component components.ComponentInterface) error {
// 	err := wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, componentReadyTimeout, true, func(ctx context.Context) (bool, error) {
// 		// TODO: see if checking deployment is a good test, CF does not create deployment
// 		appList, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(ctx, metav1.ListOptions{
// 			LabelSelector: labels.ODH.Component(component.GetComponentName()),
// 		})
// 		if err != nil {
// 			log.Printf("error listing component deployments :%v", err)
// 			return false, fmt.Errorf("error listing component deployments :%w", err)
// 		}
// 		if len(appList.Items) != 0 {
// 			if component.GetManagementState() == operatorv1.Removed {
// 				// deployment exists for removed component, retrying
// 				return false, nil
// 			}

// 			for _, deployment := range appList.Items {
// 				if deployment.Status.ReadyReplicas < 1 {
// 					log.Printf("waiting for component deployments to be in Ready state: %s", deployment.Name)
// 					return false, nil
// 				}
// 			}
// 			return true, nil
// 		}
// 		// when no deployment is found
// 		// It's ok not to have deployements for unmanaged component
// 		if component.GetManagementState() != operatorv1.Managed {
// 			return true, nil
// 		}

// 		return false, nil
// 	})

// 	return err
// }

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

const testNs = "test-model-registries"

func (tc *testContext) validateModelRegistryConfig() error {
	// check immutable property registriesNamespace
	if tc.testDsc.Spec.Components.ModelRegistry.ManagementState != operatorv1.Managed {
		// allowed to set registriesNamespace to non-default
		err := patchRegistriesNamespace(tc, testNs, testNs, false)
		if err != nil {
			return err
		}
		// allowed to set registriesNamespace back to default value
		err = patchRegistriesNamespace(tc, modelregistryctrl.DefaultModelRegistriesNamespace,
			modelregistryctrl.DefaultModelRegistriesNamespace, false)
		if err != nil {
			return err
		}
	} else {
		// not allowed to change registriesNamespace
		err := patchRegistriesNamespace(tc, testNs, modelregistryctrl.DefaultModelRegistriesNamespace, true)
		if err != nil {
			return err
		}
	}
	return nil
}

func patchRegistriesNamespace(tc *testContext, namespace string, expected string, expectErr bool) error {
	patchStr := fmt.Sprintf("{\"spec\":{\"components\":{\"modelregistry\":{\"registriesNamespace\":\"%s\"}}}}", namespace)
	err := tc.customClient.Patch(tc.ctx, tc.testDsc, client.RawPatch(types.MergePatchType, []byte(patchStr)))
	if err != nil {
		if !expectErr {
			return fmt.Errorf("unexpected error when setting registriesNamespace in DSC %s to %s: %w",
				tc.testDsc.Name, namespace, err)
		}
	} else {
		if expectErr {
			return fmt.Errorf("unexpected success when setting registriesNamespace in DSC %s to %s",
				tc.testDsc.Name, namespace)
		}
	}
	// compare expected against returned registriesNamespace
	if tc.testDsc.Spec.Components.ModelRegistry.RegistriesNamespace != expected {
		return fmt.Errorf("expected registriesNamespace %s, got %s",
			expected, tc.testDsc.Spec.Components.ModelRegistry.RegistriesNamespace)
	}
	return nil
}
