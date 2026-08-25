//go:build integration

package upgrades_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

// TestUpgradeGatesVersionGating exercises the version-gating branch of the
// gate driver (CheckUpgradeGatesInNamespace): gates are only evaluated when the
// deployed release is 2.x. For same-major upgrades and fresh installs the driver
// writes an empty acks ConfigMap and lets provisioning proceed without blocking,
// even when a normally-blocking condition (cert-manager absent) is present.
//
// This is gate-agnostic machinery: no specific gate is involved, only the
// short-circuit that skips the whole gate set.
func TestUpgradeGatesVersionGating(t *testing.T) {
	t.Run("same_major_upgrade_skips_gating", func(t *testing.T) {
		// Deployed 3.4.0 -> target 3.5.1 is a same-major upgrade. cert-manager
		// is intentionally absent: were gating active it would block, so the
		// advance proves the short-circuit fired.
		h := setupUpgradeGateTestWithReleases(t, "3.4.0", nil)

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
			HaveNoUpgradeGates(),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(And(
			HaveReleaseVersion(targetVersion),
			Not(HaveStatusCondition(
				status.ConditionTypeProvisioningProgress,
				metav1.ConditionFalse,
				status.AdminAckRequiredReason))),
		)
	})

	t.Run("fresh_install_skips_gating", func(t *testing.T) {
		// Empty deployed release simulates a fresh install (no prior
		// DSC/DSCI status.release), which resolves to major 0 and skips gating.
		h := setupUpgradeGateTestWithReleases(t, "", nil)

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
			HaveNoUpgradeGates(),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(And(
			HaveReleaseVersion(targetVersion),
			Not(HaveStatusCondition(
				status.ConditionTypeProvisioningProgress,
				metav1.ConditionFalse,
				status.AdminAckRequiredReason))),
		)
	})
}

// TestUpgradeGatesAutoAckRemovedComponent exercises the Removed-component
// short-circuit in the auto-ack driver (AutoAcknowledgeUpgradeGatesInNamespace):
// a gate whose key resolves to a DSC component that is not Managed is
// acknowledged without running its registered check.
//
// Ray is used as a self-contained representative: a RayCluster carrying the
// ray.openshift.ai/oauth-finalizer would fail the ray gate check, yet because
// ray is Removed in the DSC (managedComponents is empty) the gate is auto-acked
// and the upgrade advances. This is gate-agnostic machinery — Ray is only a
// stand-in for the Removed branch.
func TestUpgradeGatesAutoAckRemovedComponent(t *testing.T) {
	// 2.x deployed release keeps gating active; cert-manager present so the
	// remaining dependency gates clear and the only interesting gate is ray.
	h := setupUpgradeGateTest(
		t,
		append(
			certManagerPresentFixtures(),
			fixture("resources/ray/raycluster-oauth.tmpl.yaml", map[string]any{
				"Name":      "legacy-raycluster",
				"Namespace": operatorNamespace,
			}),
		)...,
	)

	// The ray gate is acked despite the blocking finalizer-carrying RayCluster,
	// because ray is Removed in the DSC — the registered check never runs.
	h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(And(
		BeAcknowledgedBy("ray"),
		HaveUnacknowledgedGateCount(0)),
	)
	h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
		HaveReleaseVersion(targetVersion),
	)
}

// TestUpgradeGatesMultipleUnacked exercises the driver with more than one gate
// blocking at once. It verifies aggregation (both gates surface as unacked and
// the upgrade is blocked), that acknowledging one gate is preserved across
// re-reconciles while the other still blocks, and that the upgrade only advances
// once every gate is acknowledged.
//
// Two non-component dependency gates are forced to block: cert-manager is absent
// and a servicemeshoperatorv2 subscription sits on a blocking channel. This is
// gate-agnostic machinery — the specific gates are just convenient blockers.
func TestUpgradeGatesMultipleUnacked(t *testing.T) {
	const (
		certManagerGate = "dependencies-cert-manager"
		serviceMeshGate = "dependencies-servicemeshoperatorv2"
	)

	// cert-manager fixtures are intentionally omitted so its gate blocks; a
	// servicemeshoperatorv2 subscription on a blocking channel blocks the second.
	h := setupUpgradeGateTest(
		t,
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
	)

	// Both gates block: two unacked entries and a blocking condition on the DSC.
	h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
		BeGatedOnlyBy(certManagerGate, serviceMeshGate),
	)
	h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(And(
		HaveReleaseVersion(deployedVersion),
		HaveStatusCondition(
			status.ConditionTypeProvisioningProgress,
			metav1.ConditionFalse,
			status.AdminAckRequiredReason)),
	)

	// Acking one gate is preserved but the other still blocks the upgrade.
	h.tf.Update(gvk.ConfigMap, upgradeAcksKey, AcknowledgeGate(certManagerGate)).Eventually().Should(
		Succeed(),
	)
	h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(And(
		BeAcknowledgedBy(certManagerGate),
		BeGatedOnlyBy(serviceMeshGate)),
	)
	h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
		HaveReleaseVersion(deployedVersion),
	)

	// Acking the last gate clears the block and the upgrade advances.
	h.tf.Update(gvk.ConfigMap, upgradeAcksKey, AcknowledgeGate(serviceMeshGate)).Eventually().Should(
		Succeed(),
	)
	h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(And(
		BeAcknowledgedBy(certManagerGate, serviceMeshGate),
		HaveUnacknowledgedGateCount(0)),
	)
	h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
		HaveReleaseVersion(targetVersion),
	)
}

// TestUpgradeGatesSelfHeal exercises the auto-ack driver's re-evaluation
// behavior: AutoAcknowledgeUpgradeGatesInNamespace re-runs each unacknowledged
// gate's registered check on every reconcile, not just once at startup. When
// the admin resolves the underlying blocker in the cluster — as opposed to
// hand-editing the odh-upgrade-acks ConfigMap — the gate is expected to clear
// on its own on a later reconcile.
//
// cert-manager is used only as a convenient, already-fixtured vehicle for a
// checkFn; the behavior under test is the re-evaluation loop itself, not the
// cert-manager gate. Creating the cert-manager namespace triggers the modules
// controller's unconditional Namespace watch, so the re-reconcile is prompt
// rather than dependent on error-backoff timing.
func TestUpgradeGatesSelfHeal(t *testing.T) {
	const certManagerGate = "dependencies-cert-manager"

	h := setupUpgradeGateTest(t)

	h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
		BeGatedOnlyBy(certManagerGate),
	)
	h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
		HaveReleaseVersion(deployedVersion),
	)

	// Fix the underlying blocker directly, without touching the acks
	// ConfigMap: install the cert-manager operator subscription.
	for _, fx := range certManagerPresentFixtures() {
		h.applyFixture(t, fx.path, fx.data)
	}

	// The gate clears on its own on a later reconcile, and the upgrade
	// advances — proving the check re-runs rather than being cached from
	// the initial evaluation.
	h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(And(
		BeAcknowledgedBy(certManagerGate),
		HaveUnacknowledgedGateCount(0)),
	)
	h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
		HaveReleaseVersion(targetVersion),
	)
}
