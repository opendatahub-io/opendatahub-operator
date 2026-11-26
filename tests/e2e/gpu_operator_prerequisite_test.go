package e2e_test

import (
	"context"
	"testing"
	"time"

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
		t.Logf("ClusterPolicy CRD %s is already installed; leaving it in place", crdName)
		tc.waitForClusterPolicySettled(t, crdName)
		tc.ensureReportedGPUOperatorPrerequisite()
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

	tc.EventuallyResourceCreated(
		WithObjectToCreate(clusterPolicyCRD()),
		WithCustomErrorMsg("Failed to create ClusterPolicy CRD %s", crdName),
	)
	createdByTest = true

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

// waitForClusterPolicySettled waits until the CRD is Established or
// Terminating. It does not decide which operator condition that state
// requires. A CRD that is still neither when the wait ends fails the test,
// because it may become Established immediately afterward.
func (tc *gpuOperatorPrerequisiteTestCtx) waitForClusterPolicySettled(t *testing.T, name string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), tc.TestTimeouts.defaultEventuallyTimeout)
	defer cancel()

	ticker := time.NewTicker(tc.TestTimeouts.defaultEventuallyPollInterval)
	defer ticker.Stop()

	for {
		if clusterPolicySettled(tc.readClusterPolicyCRD(t, name)) {
			return
		}

		select {
		case <-ctx.Done():
			if clusterPolicySettled(tc.readClusterPolicyCRD(t, name)) {
				return
			}
			t.Fatalf("ClusterPolicy CRD %s did not become Established", name)
		case <-ticker.C:
		}
	}
}

func clusterPolicySettled(crd *apiextv1.CustomResourceDefinition) bool {
	return crdConditionTrue(crd, apiextv1.Established) || crdConditionTrue(crd, apiextv1.Terminating)
}

func (tc *gpuOperatorPrerequisiteTestCtx) readClusterPolicyCRD(t *testing.T, name string) *apiextv1.CustomResourceDefinition {
	t.Helper()

	crd := &apiextv1.CustomResourceDefinition{}
	err := tc.Client().Get(t.Context(), types.NamespacedName{Name: name}, crd)
	require.NoError(t, err, "failed to read ClusterPolicy CRD %s", name)
	return crd
}

func crdConditionTrue(crd *apiextv1.CustomResourceDefinition, conditionType apiextv1.CustomResourceDefinitionConditionType) bool {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == conditionType && condition.Status == apiextv1.ConditionTrue {
			return true
		}
	}
	return false
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

// ensureReportedGPUOperatorPrerequisite accepts the condition the operator
// published after the CRD settled. Met and NotUsable are both valid; this
// path does not re-derive which one the CRD should produce.
func (tc *gpuOperatorPrerequisiteTestCtx) ensureReportedGPUOperatorPrerequisite() {
	expression := `.status.conditions[] | select(.type == "%s") | (.status == "%s" and .reason == "%s") or (.status == "%s" and .reason == "%s" and .severity == "%s")`
	args := []any{
		status.ConditionGPUOperatorPrerequisiteAvailable,
		metav1.ConditionTrue, status.GPUOperatorPrerequisiteMetReason,
		metav1.ConditionFalse, status.GPUOperatorPrerequisiteNotUsableReason, string(common.ConditionSeverityInfo),
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
