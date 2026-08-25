//go:build integration

package upgrades_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

func TestUpgradeGatesForKueueOperatorDependency(t *testing.T) {
	t.Run("blocks_when_managed", func(t *testing.T) {
		h := setupUpgradeGateTest(
			t,
			append(
				certManagerPresentFixtures(),
				fixture("resources/kueue/kueue.tmpl.yaml", map[string]any{
					"ManagementState": "Managed",
				}),
			)...,
		)

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
			BeGatedOnlyBy("dependencies-kueue-operator"),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
			HaveReleaseVersion(deployedVersion),
		)
	})

	t.Run("blocks_when_subscription_missing", func(t *testing.T) {
		h := setupUpgradeGateTest(
			t,
			append(
				certManagerPresentFixtures(),
				fixture("resources/kueue/kueue.tmpl.yaml", map[string]any{
					"ManagementState": "Unmanaged",
				}),
			)...,
		)

		h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
			BeGatedOnlyBy("dependencies-kueue-operator"),
		)
		h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
			HaveReleaseVersion(deployedVersion),
		)
	})
}

func TestUpgradeGatesForKueueComponent(t *testing.T) {
	for _, workload := range []struct {
		name         string
		templatePath string
		objectName   string
	}{
		{
			name:         "notebook",
			templatePath: "resources/kueue/notebook.tmpl.yaml",
			objectName:   "queued-notebook",
		},
		{
			name:         "inferenceservice",
			templatePath: "resources/kueue/inferenceservice.tmpl.yaml",
			objectName:   "queued-isvc",
		},
		{
			name:         "llminferenceservice",
			templatePath: "resources/kueue/llminferenceservice.tmpl.yaml",
			objectName:   "queued-llmisvc",
		},
		{
			name:         "raycluster",
			templatePath: "resources/kueue/raycluster.tmpl.yaml",
			objectName:   "queued-raycluster",
		},
		{
			name:         "rayjob",
			templatePath: "resources/kueue/rayjob.tmpl.yaml",
			objectName:   "queued-rayjob",
		},
		{
			name:         "pytorchjob",
			templatePath: "resources/kueue/pytorchjob.tmpl.yaml",
			objectName:   "queued-pytorchjob",
		},
	} {
		t.Run(workload.name, func(t *testing.T) {
			t.Run("blocks_when_unmanaged_workloads_namespace_is_missing_label", func(t *testing.T) {
				namespace := "workloads-" + workload.name + "-missing-label"
				h := setupKueueComponentUpgradeTest(
					t,
					namespace,
					map[string]string{},
					fixture(workload.templatePath, map[string]any{
						"Name":      workload.objectName,
						"Namespace": namespace,
						"QueueName": "user-queue",
					}),
				)

				h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(
					BeGatedOnlyBy("kueue"),
				)
				h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(And(
					HaveReleaseVersion(deployedVersion),
					HaveStatusCondition(
						status.ConditionTypeProvisioningProgress,
						metav1.ConditionFalse,
						status.AdminAckRequiredReason)),
				)
			})

			t.Run("advances_when_unmanaged_workloads_namespace_is_kueue_managed", func(t *testing.T) {
				namespace := "workloads-" + workload.name + "-with-label"
				h := setupKueueComponentUpgradeTest(
					t,
					namespace,
					map[string]string{cluster.KueueManagedLabelKey: "true"},
					fixture(workload.templatePath, map[string]any{
						"Name":      workload.objectName,
						"Namespace": namespace,
						"QueueName": "user-queue",
					}),
				)

				h.tf.Get(gvk.ConfigMap, upgradeAcksKey).Eventually().Should(And(
					BeAcknowledgedBy("kueue"),
					HaveUnacknowledgedGateCount(0)),
				)
				h.tf.Get(gvk.DataScienceCluster, defaultDSCKey).Eventually().Should(
					HaveReleaseVersion(targetVersion),
				)
			})
		})
	}
}

func setupKueueComponentUpgradeTest(
	t *testing.T,
	workloadNamespace string,
	namespaceLabels map[string]string,
	workloadFixture fixtureSpec,
) *upgradeGateHarness {
	t.Helper()

	return setupUpgradeGateTestWithManagedComponents(
		t,
		[]string{"kueue"},
		append(
			certManagerPresentFixtures(),
			fixture("resources/kueue/kueue.tmpl.yaml", map[string]any{
				"ManagementState": "Unmanaged",
			}),
			fixture("resources/namespace.tmpl.yaml", map[string]any{
				"Name":   "openshift-kueue-operator",
				"Labels": map[string]string{},
			}),
			fixture("resources/subscription.tmpl.yaml", map[string]any{
				"Name":         "kueue-operator",
				"Namespace":    "openshift-kueue-operator",
				"Channel":      "",
				"InstalledCSV": "",
			}),
			fixture("resources/namespace.tmpl.yaml", map[string]any{
				"Name":   workloadNamespace,
				"Labels": namespaceLabels,
			}),
			workloadFixture,
		)...,
	)
}
