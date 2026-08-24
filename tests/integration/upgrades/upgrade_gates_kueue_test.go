//go:build integration

package upgrades_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

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

		h.assertSingleBlockedGate(t, "dependencies-kueue-operator")
		h.assertDSCVersion(t, deployedVersion)
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

		h.assertSingleBlockedGate(t, "dependencies-kueue-operator")
		h.assertDSCVersion(t, deployedVersion)
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
		workload := workload

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

				h.assertSingleBlockedGate(t, "kueue")
				h.assertDSCVersion(t, deployedVersion)
				h.assertBlockingCondition(t)
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

				h.tf.Get(
					gvk.ConfigMap,
					types.NamespacedName{Name: gates.AcksConfigMap, Namespace: operatorNamespace},
				).Eventually().Should(And(
					jq.Match(`.data["%s"] == "true"`, ackKey("kueue")),
					jq.Match(`[.data | to_entries[] | select(.value != "true")] | length == 0`),
				))

				h.assertDSCVersion(t, targetVersion)
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
