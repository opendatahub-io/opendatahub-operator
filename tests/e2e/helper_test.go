package e2e_test

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

func makeDSCIObject(name string) *dsci.DSCInitialization {
	dsciTest := &dsci.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dsci.DSCInitializationSpec{
			ApplicationsNamespace: "opendatahub",
			Monitoring: dsci.Monitoring{
				ManagementState: "Managed",
				Namespace:       "opendatahub",
			},
			TrustedCABundle: dsci.TrustedCABundleSpec{
				ManagementState: "Managed",
				CustomCABundle:  "",
			},
		},
	}
	return dsciTest
}

func makeDSCObject(name string) *dsc.DataScienceCluster {
	dscTest := &dsc.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dsc.DataScienceClusterSpec{
			Components: dsc.Components{
				// keep dashboard as enabled, because other test is rely on this
				Dashboard: dashboard.Dashboard{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Workbenches: workbenches.Workbenches{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				ModelMeshServing: modelmeshserving.ModelMeshServing{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Kserve: kserve.Kserve{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
					Serving: infrav1.ServingSpec{
						ManagementState: operatorv1.Unmanaged,
					},
				},
				CodeFlare: codeflare.CodeFlare{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				Ray: ray.Ray{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Kueue: kueue.Kueue{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				ModelRegistry: modelregistry.ModelRegistry{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	}

	return dscTest
}

func isDeploymentReady(d *appsv1.Deployment) bool {
	for _, condition := range d.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable {
			if condition.Status == corev1.ConditionTrue && d.Status.ReadyReplicas > 0 {
				return true
			}
		}
	}
	return false
}

func (tc *testContext) getComponentByType(t *testing.T, c components.ComponentInterface) components.ComponentInterface { //nolint:ireturn
	t.Helper()

	value := reflect.ValueOf(c)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	name := value.Type().Name()

	comps := &tc.testDSC.Spec.Components
	field := reflect.ValueOf(comps).Elem().FieldByName(name)

	require.False(t, field.IsZero())
	return field.Addr().Interface().(components.ComponentInterface) //nolint:forcetypeassert
}

func (tc *testContext) forEachComponent(t *testing.T, f func(*testing.T, components.ComponentInterface)) {
	t.Helper()

	var d *dsc.DataScienceCluster

	if tc.testDSC != nil {
		d = tc.testDSC
	} else {
		d = &dsc.DataScienceCluster{}
	}

	allComponents, err := d.GetComponents()
	require.NoError(t, err)

	for _, c := range allComponents {
		f(t, c)
	}
}

func (tc *testContext) deploymentReplicasEq(t *testing.T, d *appsv1.Deployment, r int32) bool {
	t.Helper()

	ns := tc.applicationsNamespace
	name := d.Name

	existedDep, err := tc.kubeClient.AppsV1().Deployments(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return false
	}

	return *existedDep.Spec.Replicas == r
}

func (tc *testContext) isNoComponent(t *testing.T, c components.ComponentInterface) bool {
	t.Helper()

	deployments := tc.getComponentDeployments(t, c)
	return len(deployments.Items) == 0
}

func (tc *testContext) getComponentDeployments(t *testing.T, c components.ComponentInterface) *appsv1.DeploymentList {
	t.Helper()

	name := c.GetComponentName()
	ns := tc.applicationsNamespace
	opts := metav1.ListOptions{
		LabelSelector: odhLabelPrefix + name,
	}

	d, err := tc.kubeClient.AppsV1().Deployments(ns).List(context.TODO(), opts)
	require.NoErrorf(t, err, "error listing component %s deployment", name)
	return d
}

func (tc *testContext) checkComponentDeployments(t *testing.T, c components.ComponentInterface) (bool, int) {
	t.Helper()

	depList := tc.getComponentDeployments(t, c)
	depLen := len(depList.Items)

	// Should not be triggered before the object created since prerequisite is DSC Ready
	if depLen == 0 {
		t.Log("Component", c.GetComponentName(), "has 0 deployments")
		return true, depLen
	}

	allDeploymentsReady := true
	for _, deployment := range depList.Items {
		if deployment.Status.ReadyReplicas < 1 {
			allDeploymentsReady = false
		}
	}

	return allDeploymentsReady, depLen
}

func (tc *testContext) checkOperatorMissing(_ *testing.T, c components.ComponentInterface) bool {
	for _, condition := range tc.testDSC.Status.Conditions {
		if strings.Contains(condition.Message, "Please install the operator before enabling "+c.GetComponentName()) {
			return true
		}
	}
	return false
}

func (tc *testContext) assertEventually(t *testing.T, f func() (bool, error), msgAndArgs ...any) bool {
	t.Helper()
	err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, true,
		func(_ context.Context) (bool, error) {
			return f()
		})

	return assert.NoError(t, err, msgAndArgs...)
}

func (tc *testContext) assertEventuallyNoComponents(t *testing.T) {
	t.Helper()

	tc.forEachComponent(t, func(t *testing.T, c components.ComponentInterface) {
		t.Helper()

		tc.assertEventually(t, func() (bool, error) {
			return tc.isNoComponent(t, c), nil
		}, "error deleting component: %v", c.GetComponentName())
	})
}

func (tc *testContext) requireEventuallyDeploymentReady(t *testing.T, name, ns string) {
	t.Helper()

	tc.requireEventually(t, func() (bool, error) {
		controllerDeployment, err := tc.kubeClient.AppsV1().Deployments(ns).Get(tc.ctx, name, metav1.GetOptions{})
		if err == nil {
			return isDeploymentReady(controllerDeployment), nil
		}

		if errors.IsNotFound(err) {
			return false, nil
		}

		t.Logf("Failed to get %s controller deployment", name)
		return false, err
	})
}

func (tc *testContext) requireEventuallyControllerDeployment(t *testing.T) {
	t.Helper()

	tc.requireEventuallyDeploymentReady(t, "opendatahub-operator-controller-manager", tc.operatorNamespace)
}

func (tc *testContext) requireEventuallyCRDStatusTrue(t *testing.T, crdName string) {
	t.Helper()

	crd := &apiextv1.CustomResourceDefinition{}
	obj := client.ObjectKey{
		Name: crdName,
	}

	tc.requireEventually(t, func() (bool, error) {
		err := tc.customClient.Get(context.TODO(), obj, crd)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			log.Printf("Failed to get CRD %s", crdName)

			return false, err
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextv1.Established {
				if condition.Status == apiextv1.ConditionTrue {
					return true, nil
				}
			}
		}
		t.Logf("Error to get CRD %s condition's matching", crdName)

		return false, nil
	})
}

func (tc *testContext) getInstalled(t *testing.T, gvk schema.GroupVersionKind) *unstructured.UnstructuredList {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	err := tc.customClient.List(tc.ctx, list)
	require.NoErrorf(t, err, "Could not get %s list", gvk.Kind)

	return list
}

func (tc *testContext) requireInstalled(t *testing.T, gvk schema.GroupVersionKind) {
	t.Helper()

	list := tc.getInstalled(t, gvk)

	require.Greaterf(t, len(list.Items), 0, "%s has not been installed", gvk.Kind)
}

func (tc *testContext) requireNotInstalled(t *testing.T, gvk schema.GroupVersionKind) {
	t.Helper()

	list := tc.getInstalled(t, gvk)

	require.Equal(t, 0, len(list.Items), "%s has been installed", gvk.Kind)
}

func (tc *testContext) requireEventually(t *testing.T, f func() (bool, error), msgAndArgs ...any) {
	t.Helper()

	if tc.assertEventually(t, f, msgAndArgs...) {
		return
	}

	t.FailNow()
}

func (tc *testContext) requireEventuallyDSCReady(t *testing.T) {
	t.Helper()
	isReady := func() (bool, error) {
		key := types.NamespacedName{Name: tc.testDSC.Name}
		dsc := tc.testDSC

		err := tc.customClient.Get(tc.ctx, key, dsc)
		if err != nil {
			return false, err
		}

		return dsc.Status.Phase == "Ready", nil
	}

	tc.requireEventually(t, isReady)
}

func (tc *testContext) requireNoDSCI(t *testing.T) {
	t.Helper()

	tc.requireNotInstalled(t, gvkDataScienceCluster)
}

func (tc *testContext) requireEventuallyNoOwnedNamespaces(t *testing.T) {
	t.Helper()
	tc.requireEventually(t, func() (bool, error) {
		namespaces, err := tc.kubeClient.CoreV1().Namespaces().List(tc.ctx, metav1.ListOptions{
			LabelSelector: cluster.ODHGeneratedNamespaceLabel,
		})

		return len(namespaces.Items) == 0, err
	}, "failed waiting for all owned namespaces to be deleted")
}

func (tc *testContext) requireComponent(t *testing.T, c components.ComponentInterface) {
	t.Helper()

	name := c.GetComponentName()

	require.Equalf(t, operatorv1.Managed, c.GetManagementState(),
		"%s spec should be in 'enabled: true' state in order to perform test", name)

	deployments := tc.getComponentDeployments(t, c)

	require.Greater(t, len(deployments.Items), 0)

	d := &deployments.Items[0]
	require.True(t, isDeploymentReady(d), "deployment is not Ready for component", d.Name)
}

func (tc *testContext) requireEventuallyComponent(t *testing.T, c components.ComponentInterface) {
	t.Helper()
	name := c.GetComponentName()
	state := c.GetManagementState()
	msgState := "disabled"

	// disabledCheck, default
	check := func(_ bool, depLen int) (bool, error) { //nolint:unparam
		// do not wait in disabled case
		// if DSC is ready there should be no deployments
		if depLen == 0 {
			return true, nil
		}

		return true, fmt.Errorf("unexpected deployments in %s", name)
	}
	// enabledCheck, default
	enabledCheck := func(deployOk bool, _ int) (bool, error) { //nolint:unparam
		if deployOk {
			return true, nil
		}
		if _, ok := c.(*kserve.Kserve); ok {
			// depedent operator error, as expected
			if tc.checkOperatorMissing(t, c) {
				return true, nil
			}
		}

		return false, nil
	}

	if state == operatorv1.Managed {
		check = enabledCheck
		msgState = "enabled"
	}

	tc.requireEventually(t, func() (bool, error) {
		deployOk, depLen := tc.checkComponentDeployments(t, c)
		return check(deployOk, depLen)
	}, "error validating component", name, "when", msgState)
}

func (tc *testContext) tryExistedDSCI() error {
	existingDSCIList := &dsci.DSCInitializationList{}

	err := tc.customClient.List(tc.ctx, existingDSCIList)
	if err != nil {
		return err
	}

	if len(existingDSCIList.Items) == 0 {
		return errors.NewNotFound(schema.GroupResource{}, "DSCI not found")
	}

	tc.testDSCI = &existingDSCIList.Items[0]
	tc.applicationsNamespace = tc.testDSCI.Spec.ApplicationsNamespace

	return nil
}

func (tc *testContext) createDSCI() error {
	testDSCI := makeDSCIObject("e2e-test-dsci")

	err := tc.customClient.Create(tc.ctx, testDSCI)
	if err != nil {
		return err
	}

	tc.testDSCI = testDSCI
	tc.applicationsNamespace = tc.testDSCI.Spec.ApplicationsNamespace

	return nil
}

func (tc *testContext) tryExistedDSC() error {
	existingDSCList := &dsc.DataScienceClusterList{}

	err := tc.customClient.List(tc.ctx, existingDSCList)
	if err != nil {
		return err
	}

	if len(existingDSCList.Items) == 0 {
		return errors.NewNotFound(schema.GroupResource{}, "DSC not found")
	}

	tc.testDSC = &existingDSCList.Items[0]
	return nil
}

func (tc *testContext) createDSC() error {
	testDSC := makeDSCObject("e2e-test")

	err := tc.customClient.Create(tc.ctx, testDSC)
	if err != nil {
		return err
	}

	tc.testDSC = testDSC
	return nil
}

func (tc *testContext) ensureDSCI(t *testing.T) {
	t.Helper()

	err := tc.tryExistedDSCI()
	if err == nil {
		return
	}

	require.True(t, errors.IsNotFound(err))

	err = tc.createDSCI()
	require.NoError(t, err)
}

func (tc *testContext) ensureDSCExists(t *testing.T) {
	t.Helper()

	err := tc.tryExistedDSC()
	if err == nil {
		return
	}

	require.True(t, errors.IsNotFound(err))

	err = tc.createDSC()
	require.NoError(t, err)
}

func (tc *testContext) ensureDSC(t *testing.T) {
	t.Helper()
	tc.ensureDSCExists(t)
	tc.requireEventuallyDSCReady(t)
}

func (tc *testContext) ensureCRs(t *testing.T) {
	t.Helper()
	tc.ensureDSCI(t)
	tc.ensureDSC(t)
}

func (tc *testContext) deleteDSC(t *testing.T) { //nolint:thelper
	err := tc.customClient.Delete(tc.ctx, tc.testDSC, &client.DeleteOptions{})
	require.NoError(t, err)
	tc.testDSC = nil
}

func (tc *testContext) removeDeletionConfigMap(_ *testing.T) {
	_ = tc.kubeClient.CoreV1().ConfigMaps(tc.operatorNamespace).Delete(context.TODO(), "delete-self-managed", metav1.DeleteOptions{})
}

func (tc *testContext) createDeletionConfigMap(t *testing.T) { //nolint:thelper
	var err error
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-self-managed",
			Namespace: tc.operatorNamespace,
			Labels: map[string]string{
				upgrade.DeleteConfigMapLabel: "true",
			},
		},
	}

	configMaps := tc.kubeClient.CoreV1().ConfigMaps(configMap.Namespace)
	if _, err = configMaps.Get(context.TODO(), configMap.Name, metav1.GetOptions{}); err != nil {
		switch {
		case errors.IsNotFound(err):
			_, err = configMaps.Create(context.TODO(), configMap, metav1.CreateOptions{})

		case errors.IsAlreadyExists(err):
			_, err = configMaps.Update(context.TODO(), configMap, metav1.UpdateOptions{})
		}
	}

	require.NoError(t, err)
}

func (tc *testContext) updateComponent(t *testing.T, c components.ComponentInterface, f func(c components.ComponentInterface)) error {
	t.Helper()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh the instance in case it was updated during the reconcile
		err := tc.customClient.Get(tc.ctx, types.NamespacedName{Name: tc.testDSC.Name}, tc.testDSC)
		require.NoError(t, err, "error getting resource")

		// Get reloads the DSC object, get the actual component
		comp := tc.getComponentByType(t, c)
		f(comp)

		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return tc.customClient.Update(context.TODO(), tc.testDSC)
	})
	return err
}

func (tc *testContext) setComponentManagementState(t *testing.T, c components.ComponentInterface, state operatorv1.ManagementState) {
	t.Helper()

	err := tc.updateComponent(t, c, func(c components.ComponentInterface) {
		c.SetManagementState(state)
	})

	require.NoError(t, err, "error updating component from 'enabled: true' to 'enabled: false'")
}

func (tc *testContext) setDeploymentReplicas(t *testing.T, d *appsv1.Deployment, r int32) {
	t.Helper()

	ns := d.Namespace
	name := d.Name
	patchedReplica := &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: autoscalingv1.ScaleSpec{
			Replicas: r,
		},
		Status: autoscalingv1.ScaleStatus{},
	}

	retrievedDep, err := tc.kubeClient.AppsV1().Deployments(ns).UpdateScale(context.TODO(), name, patchedReplica, metav1.UpdateOptions{})
	require.NoErrorf(t, err, "error patching component resources : %v", err)
	require.Equalf(t, patchedReplica.Spec.Replicas, retrievedDep.Spec.Replicas,
		"failed to patch replicas : expect to be %v but got %v", patchedReplica.Spec.Replicas, retrievedDep.Spec.Replicas)
}
