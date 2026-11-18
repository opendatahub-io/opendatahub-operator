package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
)

func gpuOperatorPrerequisiteTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	tc.SkipIfXKSCluster(t)

	RunTestCases(t, []TestCase{
		{"GPU Operator prerequisite condition follows the ClusterPolicy CRD", validateGPUOperatorPrerequisite},
	})
}

type gpuOperatorPrerequisiteTestCtx struct {
	*TestContext
}

func validateGPUOperatorPrerequisite(t *testing.T) {
	t.Helper()

	base, err := NewTestContext(t)
	require.NoError(t, err)
	tc := &gpuOperatorPrerequisiteTestCtx{TestContext: base}
	tc.skipUnlessPlatformSingletons(t)

	crdName := gvk.GPUOperatorClusterPolicyCRDName
	existing := &apiextv1.CustomResourceDefinition{}
	err = tc.Client().Get(t.Context(), types.NamespacedName{Name: crdName}, existing)
	if err == nil {
		t.Logf("ClusterPolicy CRD %s is already installed; checking the met condition and leaving it in place", crdName)
		tc.ensureGPUOperatorPrerequisite(metav1.ConditionTrue, status.GPUOperatorPrerequisiteMetReason, "")
		return
	}
	require.True(t, k8serr.IsNotFound(err), "failed to read ClusterPolicy CRD: %v", err)

	tc.ensureGPUOperatorPrerequisite(metav1.ConditionFalse, status.GPUOperatorPrerequisiteMissingReason, string(common.ConditionSeverityInfo))

	createdByTest := false
	t.Cleanup(func() {
		if !createdByTest {
			return
		}
		tc.DeleteResource(
			WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdName}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	})

	createdByTest = true
	tc.EventuallyResourceCreated(
		WithObjectToCreate(clusterPolicyCRD()),
		WithCustomErrorMsg("Failed to create ClusterPolicy CRD %s", crdName),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdName}),
		WithCondition(jq.Match(`any(.status.conditions[]?; .type == "Established" and .status == "True")`)),
	)
	tc.ensureGPUOperatorPrerequisite(metav1.ConditionTrue, status.GPUOperatorPrerequisiteMetReason, "")

	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdName}),
		WithWaitForDeletion(true),
	)
	tc.ensureGPUOperatorPrerequisite(metav1.ConditionFalse, status.GPUOperatorPrerequisiteMissingReason, string(common.ConditionSeverityInfo))
}

func (tc *gpuOperatorPrerequisiteTestCtx) skipUnlessPlatformSingletons(t *testing.T) {
	t.Helper()

	for _, target := range []struct {
		kind schema.GroupVersionKind
		nn   types.NamespacedName
	}{
		{gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName},
		{gvk.DSCInitialization, tc.DSCInitializationNamespacedName},
	} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(target.kind)
		err := tc.Client().Get(t.Context(), target.nn, obj)
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			t.Skipf("skipping GPU Operator prerequisite check: %s %s is not present", target.kind.Kind, target.nn.Name)
		}
		require.NoError(t, err, "failed to read %s %s", target.kind.Kind, target.nn.Name)
	}
}

func (tc *gpuOperatorPrerequisiteTestCtx) ensureGPUOperatorPrerequisite(condStatus metav1.ConditionStatus, reason, severity string) {
	expression := `.status.conditions[] | select(.type == "%s") | .status == "%s" and .reason == "%s"`
	args := []any{status.ConditionGPUOperatorPrerequisiteAvailable, condStatus, reason}
	if severity != "" {
		expression += ` and .severity == "%s"`
		args = append(args, severity)
	}

	for _, target := range []struct {
		kind schema.GroupVersionKind
		nn   types.NamespacedName
	}{
		{gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName},
		{gvk.DSCInitialization, tc.DSCInitializationNamespacedName},
	} {
		tc.EnsureResourceExists(
			WithMinimalObject(target.kind, target.nn),
			WithCondition(jq.Match(expression, args...)),
		)
	}
}

func clusterPolicyCRD() *apiextv1.CustomResourceDefinition {
	crd := mocks.NewMockCRD(
		gvk.GPUOperatorClusterPolicy.Group,
		gvk.GPUOperatorClusterPolicy.Version,
		gvk.GPUOperatorClusterPolicy.Kind,
		"gpu-operator",
	)
	crd.TypeMeta = metav1.TypeMeta{
		APIVersion: apiextv1.SchemeGroupVersion.String(),
		Kind:       "CustomResourceDefinition",
	}
	// NewMockCRD appends "s", which is not the NVIDIA plural.
	crd.Name = gvk.GPUOperatorClusterPolicyCRDName
	crd.Spec.Names.Plural = "clusterpolicies"
	crd.Spec.Names.Singular = "clusterpolicy"
	return crd
}
