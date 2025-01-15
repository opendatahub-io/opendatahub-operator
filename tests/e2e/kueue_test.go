package e2e_test

import (
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate Kueue Dynamically create VAP and VAPB", componentCtx.validateKueueVAPReady)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

type KueueTestCtx struct {
	*ComponentTestCtx
}

func (tc *KueueTestCtx) validateKueueVAPReady(t *testing.T) {
	g := tc.NewWithT(t)
	v, err := tc.GetClusterVersion()
	g.Expect(err).ToNot(HaveOccurred())
	if v.GTE(semver.MustParse("4.17.0")) {
		g.Get(gvk.ValidatingAdmissionPolicy, types.NamespacedName{Name: "kueue-validating-admission-policy"}).Eventually().WithTimeout(1 * time.Second).
			Should(
				jq.Match(`.metadata.ownerReferences[0].name == "%s"`, componentApi.KueueInstanceName),
			)
		g.Get(gvk.ValidatingAdmissionPolicyBinding, types.NamespacedName{Name: "kueue-validating-admission-policy-binding"}).Eventually().WithTimeout(1 * time.Second).Should(
			jq.Match(`.metadata.ownerReferences | length == 0`),
		)
		return
	}
	_, err = g.Get(gvk.ValidatingAdmissionPolicy, types.NamespacedName{Name: "kueue-validating-admission-policy"}).Get()
	g.Expect(err).Should(MatchError(&meta.NoKindMatchError{}))
	_, err = g.Get(gvk.ValidatingAdmissionPolicyBinding, types.NamespacedName{Name: "kueue-validating-admission-policy-binding"}).Get()
	g.Expect(err).Should(MatchError(&meta.NoKindMatchError{}))
}
