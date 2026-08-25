//go:build integration

package upgrades_test

import (
	"testing"

	gtypes "github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// BeGatedBy matches when every named upgrade gate exists and is unacknowledged.
func BeGatedBy(keys ...string) gtypes.GomegaMatcher {
	matchers := make([]gtypes.GomegaMatcher, 0, len(keys))
	for _, key := range keys {
		matchers = append(matchers, HaveKeyWithValue(ackKey(key), Not(Equal("true"))))
	}

	return WithTransform(jq.Extract(`.data // {}`), And(matchers...))
}

// BeAcknowledgedBy matches when every named upgrade gate exists and is acknowledged.
func BeAcknowledgedBy(keys ...string) gtypes.GomegaMatcher {
	matchers := make([]gtypes.GomegaMatcher, 0, len(keys))
	for _, key := range keys {
		matchers = append(matchers, HaveKeyWithValue(ackKey(key), Equal("true")))
	}

	return WithTransform(jq.Extract(`.data // {}`), And(matchers...))
}

// BeGatedOnlyBy matches when exactly the named upgrade gates are unacknowledged.
func BeGatedOnlyBy(keys ...string) gtypes.GomegaMatcher {
	return And(
		BeGatedBy(keys...),
		HaveUnacknowledgedGateCount(len(keys)),
	)
}

// HaveUnacknowledgedGateCount matches the number of unacknowledged upgrade gates.
func HaveUnacknowledgedGateCount(expected int) gtypes.GomegaMatcher {
	return jq.Match(
		`[(.data // {}) | to_entries[] | select(.value != "true")] | length == %d`,
		expected,
	)
}

// HaveNoUpgradeGates matches an upgrade-acks ConfigMap with no gate entries.
func HaveNoUpgradeGates() gtypes.GomegaMatcher {
	return WithTransform(jq.Extract(`.data // {}`), BeEmpty())
}

// HaveReleaseVersion matches a resource with the expected status release version.
func HaveReleaseVersion(expected string) gtypes.GomegaMatcher {
	return jq.Match(`.status.release.version == %q`, expected)
}

// HaveStatusCondition matches a status condition by type, status, and reason.
func HaveStatusCondition(
	conditionType string,
	conditionStatus metav1.ConditionStatus,
	reason string,
) gtypes.GomegaMatcher {
	return WithTransform(
		jq.Extract(`.status.conditions // []`),
		ContainElement(And(
			HaveKeyWithValue("type", conditionType),
			HaveKeyWithValue("status", string(conditionStatus)),
			HaveKeyWithValue("reason", reason),
		)),
	)
}

// AcknowledgeGate transforms an upgrade-acks ConfigMap by acknowledging a gate.
func AcknowledgeGate(key string) testf.TransformFn {
	return testf.Transform(`.data["%s"] = "true"`, ackKey(key))
}

func TestUpgradeGateMatchers(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	obj := &unstructured.Unstructured{Object: map[string]any{
		"data": map[string]any{
			ackKey("acknowledged"): "true",
			ackKey("blocked-a"):    "blocked",
			ackKey("blocked-b"):    "blocked",
		},
	}}

	g.Expect(obj).To(BeAcknowledgedBy("acknowledged"))
	g.Expect(obj).To(BeGatedBy("blocked-a", "blocked-b"))
	g.Expect(obj).To(BeGatedOnlyBy("blocked-a", "blocked-b"))
	g.Expect(obj).To(HaveUnacknowledgedGateCount(2))
	g.Expect(obj).NotTo(BeGatedBy("missing"))
	g.Expect(obj).NotTo(BeGatedOnlyBy("blocked-a"))
	g.Expect(&unstructured.Unstructured{}).To(HaveNoUpgradeGates())
}

func TestUpgradeResourceMatchers(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	obj := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"release": map[string]any{"version": targetVersion},
			"conditions": []any{
				map[string]any{
					"type":   "Progressing",
					"status": "False",
					"reason": "AdminAckRequired",
				},
			},
		},
	}}

	g.Expect(obj).To(HaveReleaseVersion(targetVersion))
	g.Expect(obj).NotTo(HaveReleaseVersion(deployedVersion))
	g.Expect(&unstructured.Unstructured{}).NotTo(HaveReleaseVersion(targetVersion))
	g.Expect(obj).To(HaveStatusCondition("Progressing", metav1.ConditionFalse, "AdminAckRequired"))
	g.Expect(obj).NotTo(HaveStatusCondition("Progressing", metav1.ConditionTrue, "AdminAckRequired"))
}
