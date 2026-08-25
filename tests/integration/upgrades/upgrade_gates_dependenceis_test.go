//go:build integration

package upgrades_test

import (
	"embed"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

const (
	operatorNamespace = "opendatahub-operator-system"
	targetVersion     = "3.5.1"
	deployedVersion   = "2.25.1"
)

//go:embed resources/*.tmpl.yaml resources/kueue/*.tmpl.yaml resources/ray/*.tmpl.yaml
var resourcesFS embed.FS

func TestUpgradeGatesForCertManagerDependency(t *testing.T) {
	t.Run("keeps_dsc_version_when_missing_until_manually_acked", func(t *testing.T) {
		h := setupUpgradeGateTest(t)
		const certManagerGate = "dependencies-cert-manager"

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
			BeGatedOnlyBy(certManagerGate),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(And(
			HaveReleaseVersion(deployedVersion),
			HaveStatusCondition(
				status.ConditionTypeProvisioningProgress,
				metav1.ConditionFalse,
				status.AdminAckRequiredReason)),
		)

		h.tf.Update(
			gvk.ConfigMap,
			upgradeAcksKey,
			AcknowledgeGate(certManagerGate),
		).Eventually().Should(
			Succeed(),
		)

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(And(
			BeAcknowledgedBy(
				"dependencies-cert-manager",
				"dependencies-kueue-operator",
				"dependencies-servicemeshoperatorv2"),
			HaveUnacknowledgedGateCount(0)),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
			HaveReleaseVersion(targetVersion),
		)
	})

	t.Run("advances_dsc_version_when_present", func(t *testing.T) {
		h := setupUpgradeGateTest(t, certManagerPresentFixtures()...)

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(And(
			BeAcknowledgedBy(
				"dependencies-cert-manager",
				"dependencies-kueue-operator",
				"dependencies-servicemeshoperatorv2"),
			HaveUnacknowledgedGateCount(0)),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
			HaveReleaseVersion(targetVersion),
		)
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

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
			BeGatedOnlyBy("dependencies-servicemeshoperatorv2"),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
			HaveReleaseVersion(deployedVersion),
		)
	})
}
