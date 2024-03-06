package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
)

// TestOdhOperator sets up the testing suite for ODH Operator.
func TestOdhOperator(t *testing.T) {
	tc, err := NewTestContext()
	require.NoError(t, err)

	t.Run("DeploymentValidation", tc.deploymentValidationTest)
	t.Run("Creation", tc.creationTest)
	t.Run("Duplication", tc.duplicationTest)
	t.Run("Validation", tc.validationTest)
	t.Run("Functional", tc.functionalTest)
	t.Run("Deletion", tc.deletionTest)
}

func (tc *testContext) deploymentValidationTest(t *testing.T) {
	t.Run("OperatorDeployment", tc.validateODHDeployment)
	t.Run("DSCICRDTrue", tc.validateCRDTrueDSCI)
	t.Run("DSCCRDTrue", tc.validateCRDTrueDSC)
}

func (tc *testContext) creationTest(t *testing.T) {
	t.Run("DSCI", tc.testDSCICreation)
	t.Run("DSC", tc.testDSCCreation)
}

func (tc *testContext) duplicationTest(t *testing.T) {
	t.Run("DSCI", tc.testDSCIDuplication)
	t.Run("DSC", tc.testDSCDuplication)
}

func (tc *testContext) validationTest(t *testing.T) {
	tc.ensureCRs(t)

	t.Run("ComponentsCreation", tc.validateComponentsCreation)
	t.Run("OwnerReferences", tc.validateOwnerRefrences)
}

func (tc *testContext) functionalTest(t *testing.T) {
	tc.ensureCRs(t)

	t.Run("Reconcile", tc.testUpdateComponentReconcile)
	t.Run("EnabledChange", tc.testUpdateDSCComponentEnabled)
}

func (tc *testContext) deletionTest(t *testing.T) {
	if skipDeletion {
		t.Skip()
	}

	t.Run("DeleteDSC", tc.testDSCDeletion)
	t.Run("testDSCDeletionUsingConfigMap", tc.testDSCDeletionUsingConfigMap)
}

func (tc *testContext) testDSCICreation(t *testing.T) {
	err := tc.tryExistedDSCI()
	if err == nil {
		t.Skip("skipping: Using existed DSCI")
	}

	require.True(t, errors.IsNotFound(err))

	err = tc.createDSCI()
	require.NoError(t, err)
}

func (tc *testContext) testDSCCreation(t *testing.T) {
	err := tc.tryExistedDSC()
	if err == nil {
		t.Skip("skipping: Using existed DSC")
	}

	require.True(t, errors.IsNotFound(err))

	err = tc.createDSC()
	require.NoError(t, err)
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

func (tc *testContext) testDSCIDuplication(t *testing.T) {
	t.Parallel()

	dup := makeDSCIObject("e2e-test-dsci-dup")
	tc.testDuplication(t, gvkDSCInitializaion, dup)
}

func (tc *testContext) testDSCDuplication(t *testing.T) {
	t.Parallel()

	dup := makeDSCObject("e2e-test-dup")
	tc.testDuplication(t, gvkDataScienceCluster, dup)
}

func (tc *testContext) validateComponentOwnerReference(t *testing.T, c components.ComponentInterface) { //nolint:thelper
	d := tc.getComponentDeployments(t, c)

	if assert.NotEqual(t, 0, len(d.Items)) {
		require.Equalf(t, "DataScienceCluster", d.Items[0].OwnerReferences[0].Kind,
			"expected ownerreference not found. Got ownereferrence: %v", d.Items[0].OwnerReferences)
	}
}

func (tc *testContext) validateOwnerRefrences(t *testing.T) {
	t.Parallel()
	// test Dashboard only
	c := &tc.testDSC.Spec.Components.Dashboard

	if c.ManagementState != operatorv1.Managed {
		t.Skip("skipping unmanaged component", c.GetComponentName())
	}

	tc.validateComponentOwnerReference(t, c)
}

func (tc *testContext) testUpdateComponentReconcile(t *testing.T) {
	// test Dashboard only
	c := &tc.testDSC.Spec.Components.Dashboard
	deployments := tc.getComponentDeployments(t, c)

	if len(deployments.Items) == 0 {
		t.Skip("no deployments for", c.GetComponentName())
	}

	d := &deployments.Items[0]
	cur := *d.Spec.Replicas

	tc.setDeploymentReplicas(t, d, cur+1)

	tc.requireEventually(t, func() (bool, error) {
		return tc.deploymentReplicasEq(t, d, cur), nil
	})
}

func (tc *testContext) testUpdateDSCComponentEnabled(t *testing.T) {
	c := &tc.testDSC.Spec.Components.Dashboard

	tc.requireComponent(t, c)
	defer tc.setComponentManagementState(t, c, operatorv1.Managed)

	tc.setComponentManagementState(t, c, operatorv1.Removed)

	tc.requireEventually(t, func() (bool, error) {
		return tc.isNoComponent(t, c), nil
	})
}

func (tc *testContext) testDSCDeletion(t *testing.T) {
	tc.ensureCRs(t)

	tc.deleteDSC(t)

	tc.assertEventuallyNoComponents(t)
}

func (tc *testContext) testDSCDeletionUsingConfigMap(t *testing.T) {
	defer tc.removeDeletionConfigMap(t)
	tc.ensureCRs(t)

	tc.createDeletionConfigMap(t)
	// why it was originally done?
	// tc.deleteDSC(t)

	tc.requireEventuallyNoOwnedNamespaces(t)
	tc.requireNoDSCI(t)
	tc.assertEventuallyNoComponents(t)
}

func (tc *testContext) validateODHDeployment(t *testing.T) {
	t.Parallel()
	tc.requireEventuallyControllerDeployment(t)
}

func (tc *testContext) validateCRDTrueDSCI(t *testing.T) {
	t.Parallel()
	tc.requireEventuallyCRDStatusTrue(t, "dscinitializations.dscinitialization.opendatahub.io")
}

func (tc *testContext) validateCRDTrueDSC(t *testing.T) {
	t.Parallel()
	tc.requireEventuallyCRDStatusTrue(t, "datascienceclusters.datasciencecluster.opendatahub.io")
}

func (tc *testContext) validateComponentsCreation(t *testing.T) {
	t.Parallel()
	tc.ensureCRs(t)

	addComponentCreationCheck := func(t *testing.T, c components.ComponentInterface) { //nolint:thelper
		name := c.GetComponentName()
		t.Run("checkDeployment_"+name, func(t *testing.T) {
			t.Parallel()

			tc.requireEventuallyComponent(t, c)
		})
	}
	tc.forEachComponent(t, addComponentCreationCheck)
}
