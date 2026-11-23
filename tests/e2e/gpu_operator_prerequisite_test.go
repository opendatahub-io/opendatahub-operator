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
		if tc.clusterPolicyBecomesMet(t, crdName) {
			tc.ensureGPUOperatorPrerequisite(metav1.ConditionTrue, status.GPUOperatorPrerequisiteMetReason, "")
		} else {
			t.Logf("ClusterPolicy CRD %s is terminating, not serving v1, or not Established; expecting NotUsable", crdName)
			tc.ensureGPUOperatorPrerequisite(metav1.ConditionFalse, status.GPUOperatorPrerequisiteNotUsableReason, string(common.ConditionSeverityInfo))
		}
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

// clusterPolicyBecomesMet reports whether the installed CRD can satisfy Met.
// Established alone is not enough: a terminating CRD or one that does not
// serve v1 returns false immediately so the caller can expect NotUsable. A
// served v1 CRD that is not terminating is polled until it is Established.
func (tc *gpuOperatorPrerequisiteTestCtx) clusterPolicyBecomesMet(t *testing.T, name string) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), tc.TestTimeouts.defaultEventuallyTimeout)
	defer cancel()

	ticker := time.NewTicker(tc.TestTimeouts.defaultEventuallyPollInterval)
	defer ticker.Stop()

	for {
		crd := &apiextv1.CustomResourceDefinition{}
		err := tc.Client().Get(t.Context(), types.NamespacedName{Name: name}, crd)
		require.NoError(t, err, "failed to read ClusterPolicy CRD %s", name)
		if crdConditionTrue(crd, apiextv1.Terminating) || !clusterPolicyServesV1(crd) {
			return false
		}
		if crdConditionTrue(crd, apiextv1.Established) {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func crdConditionTrue(crd *apiextv1.CustomResourceDefinition, conditionType apiextv1.CustomResourceDefinitionConditionType) bool {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == conditionType && condition.Status == apiextv1.ConditionTrue {
			return true
		}
	}
	return false
}

func clusterPolicyServesV1(crd *apiextv1.CustomResourceDefinition) bool {
	for _, version := range crd.Spec.Versions {
		if version.Name == gvk.GPUOperatorClusterPolicy.Version && version.Served {
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
