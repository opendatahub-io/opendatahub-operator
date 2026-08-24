//go:build integration

package upgrades_test

import (
	"embed"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const (
	operatorNamespace = "opendatahub-operator-system"
	targetVersion     = "3.5.1"
	deployedVersion   = "2.25.1"
)

//go:embed resources/*.tmpl.yaml resources/kueue/*.tmpl.yaml
var resourcesFS embed.FS

func TestUpgradeGatesForCertManagerDependency(t *testing.T) {
	t.Run("keeps_dsc_version_when_missing_until_manually_acked", func(t *testing.T) {
		h := setupUpgradeGateTest(t)
		acksKey := types.NamespacedName{Name: gates.AcksConfigMap, Namespace: operatorNamespace}
		certManagerAckKey := ackKey("dependencies-cert-manager")

		h.tf.Get(
			gvk.ConfigMap,
			acksKey,
		).Eventually().Should(And(
			jq.Match(`(.data | keys | length) > 0`),
			jq.Match(`[.data | keys[] | select(startswith("ack-%s-"))] | length > 0`, targetVersion),
			jq.Match(`.data["%s"] != "true"`, certManagerAckKey),
			jq.Match(`[.data | to_entries[] | select(.value != "true")] | length == 1`),
		))

		h.assertDSCVersion(t, deployedVersion)
		h.assertBlockingCondition(t)

		h.tf.Update(
			gvk.ConfigMap,
			acksKey,
			func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, "true", "data", certManagerAckKey)
			},
		).Eventually().Should(Succeed())

		h.assertAllDependencyGatesAcknowledged(t)
		h.assertDSCVersion(t, targetVersion)
	})

	t.Run("advances_dsc_version_when_present", func(t *testing.T) {
		h := setupUpgradeGateTest(t, certManagerPresentFixtures()...)

		h.assertAllDependencyGatesAcknowledged(t)
		h.assertDSCVersion(t, targetVersion)
	})
}

func TestUpgradeGatesForServiceMeshOperatorV2Dependency(t *testing.T) {
	t.Run("blocks_when_subscription_exists_on_blocking_channel", func(t *testing.T) {
		h := setupUpgradeGateTest(
			t,
			append(
				certManagerPresentFixtures(),
				fixture("resources/namespace.tmpl.yaml", map[string]any{
					"Name":   "istio-system",
					"Labels": map[string]string{},
				}),
				fixture("resources/subscription.tmpl.yaml", map[string]any{
					"Name":         "servicemeshoperatorv2",
					"Namespace":    "istio-system",
					"Channel":      "stable",
					"InstalledCSV": "servicemeshoperatorv2.v2.6.0",
				}),
			)...,
		)

		h.assertSingleBlockedGate(t, "dependencies-servicemeshoperatorv2")
		h.assertDSCVersion(t, deployedVersion)
	})
}
