package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

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
	t.Run("Validate CRDs reinstated", componentCtx.validateCRDReinstated)
	t.Run("Validate pre check", componentCtx.validateKueuePreCheck)
	t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
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

func (tc *KueueTestCtx) validateCRDReinstated(t *testing.T) {
	// validate multikuueclusters, multikueueconfigs has v1beta1 version

	crds := map[string]string{
		"workloads.kueue.x-k8s.io":          "v1beta1",
		"multikueueclusters.kueue.x-k8s.io": "v1beta1",
		"multikueueconfigs.kueue.x-k8s.io":  "v1beta1",
	}

	for crd, version := range crds {
		t.Run(crd, func(t *testing.T) {
			tc.ValidateCRDReinstated(t, crd, version)
		})
	}
}

func (tc *KueueTestCtx) validateKueuePreCheck(t *testing.T) {
	// validate precheck on CRD version
	// step:
	// delete crd, check it is gone, then install old crd,
	// set kueue to managed, result to error, delete crd, result to success.

	g := tc.NewWithT(t)

	g.Update(
		gvk.DataScienceCluster,
		tc.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(tc.GVK.Kind), operatorv1.Removed),
	).Eventually().Should(
		Succeed(),
	)

	g.List(tc.GVK).Eventually().Should(
		BeEmpty())

	var mkConfig = "multikueueconfigs.kueue.x-k8s.io"
	var mkCluster = "multikueueclusters.kueue.x-k8s.io"

	g.Delete(gvk.CustomResourceDefinition,
		types.NamespacedName{Name: mkCluster},
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	).Eventually().Should(
		Succeed(),
	)
	g.Eventually(func() bool {
		var crd extv1.CustomResourceDefinition
		err := tc.Client().Get(context.Background(), types.NamespacedName{Name: mkCluster}, &crd)
		return k8serr.IsNotFound(err)
	}).Should(BeTrue())

	g.Delete(gvk.CustomResourceDefinition,
		types.NamespacedName{Name: mkConfig},
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	).Eventually().Should(
		Succeed(),
	)
	g.Eventually(func() bool {
		var crd extv1.CustomResourceDefinition
		err := tc.Client().Get(context.Background(), types.NamespacedName{Name: mkConfig}, &crd)
		return k8serr.IsNotFound(err)
	}).Should(BeTrue())

	c1 := mockCRDcreation("kueue.x-k8s.io", "v1alpha1", "multikueuecluster", "kueue")
	e1 := tc.Client().Create(context.Background(), c1)
	g.Expect(e1).ToNot(HaveOccurred())

	c2 := mockCRDcreation("kueue.x-k8s.io", "v1alpha1", "multikueueconfig", "kueue")
	e2 := tc.Client().Create(context.Background(), c2)
	g.Expect(e2).ToNot(HaveOccurred())

	g.Update(
		gvk.DataScienceCluster,
		tc.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(tc.GVK.Kind), operatorv1.Managed),
	).Eventually().Should(
		Succeed(),
	)

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, strings.ToLower(tc.GVK.Kind), operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
		)),
	))

	g.Delete(gvk.CustomResourceDefinition,
		types.NamespacedName{Name: mkCluster},
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	).Eventually().Should(
		Succeed(),
	)
	g.Delete(gvk.CustomResourceDefinition,
		types.NamespacedName{Name: mkConfig},
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	).Eventually().Should(
		Succeed(),
	)

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		),
	))
}
