package e2e_test

import (
	"testing"

	"github.com/blang/semver/v4"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func kueueTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.Kueue{})
	require.NoError(t, err)

	componentCtx := KueueTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate Kueue Dynamically create VAP", componentCtx.validateKueueVAPReady)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

type KueueTestCtx struct {
	*ComponentTestCtx
}

func (tc *KueueTestCtx) validateKueueVAPReady(t *testing.T) {
	g := tc.NewWithT(t)
	if cluster.GetClusterInfo().Version.GTE(semver.MustParse("4.17.0")) {
		g.Get(gvk.ValidatingAdmissionPolicy, types.NamespacedName{Name: "kueue-validating-admission-policy"}).Eventually().Should(
			jq.Match(`.metadata.ownerReferences == "%s"`, componentApi.KueueInstanceName),
		)
		vapb, err := g.Get(gvk.ValidatingAdmissionPolicyBinding, types.NamespacedName{Name: "kueue-validating-admission-policy-binding"}).Get()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(vapb.GetOwnerReferences()).Should(BeEmpty())
		return
	}
	scheme := runtime.NewScheme()
	vap := &unstructured.Unstructured{}
	vap.SetKind(gvk.ValidatingAdmissionPolicy.Kind)
	err := resources.EnsureGroupVersionKind(scheme, vap)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to get GVK"))

	vapb := &unstructured.Unstructured{}
	vapb.SetKind(gvk.ValidatingAdmissionPolicyBinding.Kind)
	err = resources.EnsureGroupVersionKind(scheme, vapb)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to get GVK"))
}
