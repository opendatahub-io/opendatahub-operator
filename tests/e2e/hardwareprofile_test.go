package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	hwpTestNamespace = "test-hardware-profile-workload"
)

type HardwareProfileWorkloadTestCtx struct {
	*TestContext
}

func hardwareProfileWorkloadTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	hwpTestCtx := HardwareProfileWorkloadTestCtx{
		TestContext: tc,
	}
	// Setup test namespace for all tests
	hwpTestCtx.setupTestNamespace()
	// Define test cases
	testCases := []TestCase{
		{"Validate HWP Toleration", hwpTestCtx.ValidateHWPToleration},
		{"Validate HWP Kueue", hwpTestCtx.ValidateHWPKueue},
		{"Validate HWP Annotation Removal", hwpTestCtx.ValidateHWPAnnotationRemoval},
		{"Validate Manual Toleration Preservation", hwpTestCtx.ValidateManualTolerationPreservation},
		{"Validate HWP Profile Switching", hwpTestCtx.ValidateHWPProfileSwitching},
		{"Validate Kueue Label Removal On HWP Removal", hwpTestCtx.ValidateKueueLabelRemovalOnHWPRemoval},
	}
	// Cleanup test namespace after tests
	defer hwpTestCtx.cleanupTestNamespace()

	// Run the test suite
	RunTestCases(t, testCases)
}

// ValidateHWPToleration tests that a workload can start with an empty profile,
// change to a profile with a toleration, and then change back to the empty profile.
func (tc *HardwareProfileWorkloadTestCtx) ValidateHWPToleration(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

	workloadName := "test-notebook-toleration"

	// Create an empty hardware profile (empty-profile)
	hwpEmpty := tc.createEmptyHardwareProfile("empty-hwp-test-toleration")

	// Create a hardware profile with tolerations and node affinity
	hwpWithToleration := tc.createHardwareProfileWithToleration("hwp-with-toleration")

	// Step 1: Start workload with empty profile
	t.Log("Step 1: Creating workload with an empty profile")
	defaultNotebook := envtestutil.NewNotebook(workloadName, hwpTestNamespace,
		envtestutil.WithHardwareProfile(hwpEmpty.Name),
		envtestutil.WithHardwareProfileNamespace(hwpEmpty.Namespace),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(defaultNotebook),
		WithCustomErrorMsg("Failed to create notebook with an empty profile"),
	)

	// Verify workload is using the empty profile (no tolerations)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithCondition(jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpEmpty.Name)),
		WithCustomErrorMsg("Workload should have empty profile annotation"),
	)

	// Step 2: Change to profile with toleration
	t.Log("Step 2: Updating workload to use profile with tolerations")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			.metadata.annotations["opendatahub.io/hardware-profile-name"] = "%s" |
			.metadata.annotations["opendatahub.io/hardware-profile-namespace"] = "%s"
		`, hwpWithToleration.Name, hwpWithToleration.Namespace)),
		WithCondition(And(
			Succeed(),
			jq.Match(`.spec.template.spec.tolerations | length > 0`),
			jq.Match(`.spec.template.spec.tolerations[0].key == "test-key"`),
			jq.Match(`.spec.template.spec.nodeSelector["kubernetes.io/os"] == "linux"`),
		)),
		WithCustomErrorMsg("Failed to update notebook with toleration profile"),
	)

	// Step 3: Change back to empty profile
	t.Log("Step 3: Updating workload back to empty profile")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			.metadata.annotations["opendatahub.io/hardware-profile-name"] = "%s" |
			.metadata.annotations["opendatahub.io/hardware-profile-namespace"] = "%s"
		`, hwpEmpty.Name, hwpEmpty.Namespace)),
		WithCondition(And(
			Succeed(),
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpEmpty.Name),
			jq.Match(`.spec.template.spec.tolerations == null or (.spec.template.spec.tolerations | length == 0)`),
			jq.Match(`.spec.template.spec.nodeSelector == null or (.spec.template.spec.nodeSelector | length == 0)`),
		)),
		WithCustomErrorMsg("Failed to update notebook back to empty profile"),
	)

	// Cleanup workload
	tc.DeleteResource(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithWaitForDeletion(true),
	)
}

// ValidateHWPKueue tests that a workload can start with a kueue hardwareprofile,
// then change to an empty HWP.
func (tc *HardwareProfileWorkloadTestCtx) ValidateHWPKueue(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

	workloadName := "test-notebook-kueue"
	// Create an empty hardware profile (empty-profile)
	hwpEmpty := tc.createEmptyHardwareProfile("empty-hwp-test-kueue")

	// Create a hardware profile with Kueue scheduling
	hwpWithKueue := tc.createHardwareProfileWithKueue("hwp-with-kueue")

	// Step 1: Start workload with Kueue profile
	t.Log("Step 1: Creating workload with Kueue hardware profile")
	kueueNotebook := envtestutil.NewNotebook(workloadName, hwpTestNamespace,
		envtestutil.WithHardwareProfile(hwpWithKueue.Name),
		envtestutil.WithHardwareProfileNamespace(hwpWithKueue.Namespace),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(kueueNotebook),
		WithCustomErrorMsg("Failed to create notebook with Kueue profile"),
	)

	// Verify workload has Kueue configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpWithKueue.Name),
			jq.Match(`.metadata.labels["kueue.x-k8s.io/queue-name"] == "%s"`, hwpWithKueue.Spec.SchedulingSpec.Kueue.LocalQueueName),
		)),
		WithCustomErrorMsg("Workload should have Kueue profile annotation and queue label"),
	)

	// Step 2: Change to empty profile
	t.Log("Step 2: Updating workload to use empty hardware profile")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			.metadata.annotations["opendatahub.io/hardware-profile-name"] = "%s" |
			.metadata.annotations["opendatahub.io/hardware-profile-namespace"] = "%s"
		`, hwpEmpty.Name, hwpEmpty.Namespace)),
		WithCondition(And(
			Succeed(),
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpEmpty.Name),
			// ensure the kueue queue label is removed (handles nil labels too)
			jq.Match(`(.metadata.labels // {} | has("kueue.x-k8s.io/queue-name") | not)`),
		)),
		WithCustomErrorMsg("Failed to update notebook to empty profile"),
	)

	// Cleanup workload
	tc.DeleteResource(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithWaitForDeletion(true),
	)
}

// ValidateHWPAnnotationRemoval tests that when the HWP annotation is removed from a workload,
// the HWP-applied tolerations and nodeSelector are cleaned up, but manually-added ones are preserved.
func (tc *HardwareProfileWorkloadTestCtx) ValidateHWPAnnotationRemoval(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

	workloadName := "test-notebook-hwp-removal"

	// Create a hardware profile with tolerations and node affinity
	hwpWithToleration := tc.createHardwareProfileWithToleration("hwp-for-removal-test")

	// Step 1: Create workload with HWP that has tolerations
	t.Log("Step 1: Creating workload with HWP that has tolerations")
	notebook := envtestutil.NewNotebook(workloadName, hwpTestNamespace,
		envtestutil.WithHardwareProfile(hwpWithToleration.Name),
		envtestutil.WithHardwareProfileNamespace(hwpWithToleration.Namespace),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(notebook),
		WithCustomErrorMsg("Failed to create notebook with HWP"),
	)

	// Verify workload has HWP-applied tolerations and nodeSelector
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpWithToleration.Name),
			jq.Match(`.spec.template.spec.tolerations | length > 0`),
			jq.Match(`.spec.template.spec.tolerations[0].key == "test-key"`),
			jq.Match(`.spec.template.spec.nodeSelector["kubernetes.io/os"] == "linux"`),
		)),
		WithCustomErrorMsg("Workload should have HWP-applied tolerations and nodeSelector"),
	)

	// Step 2: Add a manual toleration that should be preserved
	t.Log("Step 2: Adding manual toleration to workload")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			.spec.template.spec.tolerations += [{"key": "manual-key", "operator": "Exists", "effect": "NoSchedule"}] |
			.spec.template.spec.nodeSelector["manual-selector"] = "manual-value"
		`)),
		WithCondition(And(
			Succeed(),
			jq.Match(`.spec.template.spec.tolerations | length == 2`),
			jq.Match(`.spec.template.spec.tolerations | map(select(.key == "manual-key")) | length == 1`),
			jq.Match(`.spec.template.spec.nodeSelector["manual-selector"] == "manual-value"`),
		)),
		WithCustomErrorMsg("Failed to add manual toleration to workload"),
	)

	// Step 3: Remove the HWP annotation
	t.Log("Step 3: Removing HWP annotation from workload")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			del(.metadata.annotations["opendatahub.io/hardware-profile-name"])
		`)),
		WithCondition(And(
			Succeed(),
			// HWP annotation should be removed
			jq.Match(`(.metadata.annotations["opendatahub.io/hardware-profile-name"] // "") == ""`),
			// HWP namespace annotation should also be removed
			jq.Match(`(.metadata.annotations["opendatahub.io/hardware-profile-namespace"] // "") == ""`),
			// Manual toleration should be preserved
			jq.Match(`.spec.template.spec.tolerations | map(select(.key == "manual-key")) | length == 1`),
			// HWP-applied toleration should be removed
			jq.Match(`.spec.template.spec.tolerations | map(select(.key == "test-key")) | length == 0`),
			// Manual nodeSelector should be preserved
			jq.Match(`.spec.template.spec.nodeSelector["manual-selector"] == "manual-value"`),
			// HWP-applied nodeSelector should be removed
			jq.Match(`(.spec.template.spec.nodeSelector["kubernetes.io/os"] // "") == ""`),
		)),
		WithCustomErrorMsg("HWP removal should clean up HWP-applied settings but preserve manual ones"),
	)

	// Cleanup workload
	tc.DeleteResource(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithWaitForDeletion(true),
	)
}

// ValidateManualTolerationPreservation tests that when a workload with existing tolerations
// gets an HWP annotation added, the manual tolerations are preserved and merged with HWP tolerations.
func (tc *HardwareProfileWorkloadTestCtx) ValidateManualTolerationPreservation(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

	workloadName := "test-notebook-manual-tol"

	// Create a hardware profile with tolerations
	hwpWithToleration := tc.createHardwareProfileWithToleration("hwp-for-manual-tol-test")

	// Step 1: Create workload WITHOUT HWP annotation but WITH manual tolerations
	t.Log("Step 1: Creating workload with manual tolerations (no HWP)")
	notebook := envtestutil.NewNotebook(workloadName, hwpTestNamespace)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(notebook),
		WithCustomErrorMsg("Failed to create notebook without HWP"),
	)

	// Step 2: Add manual tolerations and nodeSelector to the workload
	t.Log("Step 2: Adding manual tolerations and nodeSelector to workload")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			.spec.template.spec.tolerations = [{"key": "manual-key", "operator": "Exists", "effect": "NoSchedule"}] |
			.spec.template.spec.nodeSelector = {"manual-selector": "manual-value"}
		`)),
		WithCondition(And(
			Succeed(),
			jq.Match(`.spec.template.spec.tolerations | length == 1`),
			jq.Match(`.spec.template.spec.tolerations[0].key == "manual-key"`),
			jq.Match(`.spec.template.spec.nodeSelector["manual-selector"] == "manual-value"`),
		)),
		WithCustomErrorMsg("Failed to add manual tolerations to workload"),
	)

	// Step 3: Add HWP annotation - manual tolerations should be preserved and merged
	t.Log("Step 3: Adding HWP annotation to workload with existing tolerations")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			.metadata.annotations["opendatahub.io/hardware-profile-name"] = "%s" |
			.metadata.annotations["opendatahub.io/hardware-profile-namespace"] = "%s"
		`, hwpWithToleration.Name, hwpWithToleration.Namespace)),
		WithCondition(And(
			Succeed(),
			// HWP annotation should be set
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpWithToleration.Name),
			// Both manual and HWP tolerations should exist (merged)
			jq.Match(`.spec.template.spec.tolerations | length == 2`),
			jq.Match(`.spec.template.spec.tolerations | map(select(.key == "manual-key")) | length == 1`),
			jq.Match(`.spec.template.spec.tolerations | map(select(.key == "test-key")) | length == 1`),
			// Both manual and HWP nodeSelector should exist (merged)
			jq.Match(`.spec.template.spec.nodeSelector["manual-selector"] == "manual-value"`),
			jq.Match(`.spec.template.spec.nodeSelector["kubernetes.io/os"] == "linux"`),
		)),
		WithCustomErrorMsg("Adding HWP should merge tolerations, not replace them"),
	)

	// Cleanup workload
	tc.DeleteResource(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithWaitForDeletion(true),
	)
}

// ValidateHWPProfileSwitching tests that when switching from one HWP to another,
// the old HWP's tolerations and nodeSelector are cleared and replaced with the new HWP's settings.
func (tc *HardwareProfileWorkloadTestCtx) ValidateHWPProfileSwitching(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

	workloadName := "test-notebook-profile-switch"

	// Create two different hardware profiles with different tolerations
	hwpFirst := tc.createHardwareProfileWithCustomToleration("hwp-first", "first-key", "first-os")
	hwpSecond := tc.createHardwareProfileWithCustomToleration("hwp-second", "second-key", "second-os")

	// Step 1: Create workload with first HWP
	t.Log("Step 1: Creating workload with first HWP")
	notebook := envtestutil.NewNotebook(workloadName, hwpTestNamespace,
		envtestutil.WithHardwareProfile(hwpFirst.Name),
		envtestutil.WithHardwareProfileNamespace(hwpFirst.Namespace),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(notebook),
		WithCustomErrorMsg("Failed to create notebook with first HWP"),
	)

	// Verify workload has first HWP's tolerations
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpFirst.Name),
			jq.Match(`.spec.template.spec.tolerations | length == 1`),
			jq.Match(`.spec.template.spec.tolerations[0].key == "first-key"`),
			jq.Match(`.spec.template.spec.nodeSelector["kubernetes.io/os"] == "first-os"`),
		)),
		WithCustomErrorMsg("Workload should have first HWP's tolerations"),
	)

	// Step 2: Switch to second HWP - old tolerations should be cleared and replaced
	t.Log("Step 2: Switching to second HWP")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			.metadata.annotations["opendatahub.io/hardware-profile-name"] = "%s" |
			.metadata.annotations["opendatahub.io/hardware-profile-namespace"] = "%s"
		`, hwpSecond.Name, hwpSecond.Namespace)),
		WithCondition(And(
			Succeed(),
			// HWP annotation should be updated
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpSecond.Name),
			// Only second HWP's toleration should exist (first one cleared)
			jq.Match(`.spec.template.spec.tolerations | length == 1`),
			jq.Match(`.spec.template.spec.tolerations[0].key == "second-key"`),
			jq.Match(`.spec.template.spec.tolerations | map(select(.key == "first-key")) | length == 0`),
			// Only second HWP's nodeSelector should exist
			jq.Match(`.spec.template.spec.nodeSelector["kubernetes.io/os"] == "second-os"`),
		)),
		WithCustomErrorMsg("Switching HWP should clear old tolerations and apply new ones"),
	)

	// Cleanup workload
	tc.DeleteResource(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithWaitForDeletion(true),
	)
}

// ValidateKueueLabelRemovalOnHWPRemoval tests that when the HWP annotation is removed
// from a workload with Kueue scheduling, the kueue.x-k8s.io/queue-name label is removed.
func (tc *HardwareProfileWorkloadTestCtx) ValidateKueueLabelRemovalOnHWPRemoval(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

	workloadName := "test-notebook-kueue-removal"

	// Create a hardware profile with Kueue scheduling
	hwpWithKueue := tc.createHardwareProfileWithKueue("hwp-kueue-for-removal")

	// Step 1: Create workload with Kueue HWP
	t.Log("Step 1: Creating workload with Kueue HWP")
	notebook := envtestutil.NewNotebook(workloadName, hwpTestNamespace,
		envtestutil.WithHardwareProfile(hwpWithKueue.Name),
		envtestutil.WithHardwareProfileNamespace(hwpWithKueue.Namespace),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(notebook),
		WithCustomErrorMsg("Failed to create notebook with Kueue HWP"),
	)

	// Verify workload has Kueue label
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.annotations["opendatahub.io/hardware-profile-name"] == "%s"`, hwpWithKueue.Name),
			jq.Match(`.metadata.labels["kueue.x-k8s.io/queue-name"] == "%s"`, hwpWithKueue.Spec.SchedulingSpec.Kueue.LocalQueueName),
		)),
		WithCustomErrorMsg("Workload should have Kueue label"),
	)

	// Step 2: Remove the HWP annotation - Kueue label should be removed
	t.Log("Step 2: Removing HWP annotation from workload")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithMutateFunc(testf.Transform(`
			del(.metadata.annotations["opendatahub.io/hardware-profile-name"])
		`)),
		WithCondition(And(
			Succeed(),
			// HWP annotation should be removed
			jq.Match(`(.metadata.annotations["opendatahub.io/hardware-profile-name"] // "") == ""`),
			// HWP namespace annotation should also be removed
			jq.Match(`(.metadata.annotations["opendatahub.io/hardware-profile-namespace"] // "") == ""`),
			// Kueue label should be removed
			jq.Match(`(.metadata.labels // {} | has("kueue.x-k8s.io/queue-name") | not)`),
		)),
		WithCustomErrorMsg("HWP removal should clean up Kueue label"),
	)

	// Cleanup workload
	tc.DeleteResource(
		WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: workloadName, Namespace: hwpTestNamespace}),
		WithWaitForDeletion(true),
	)
}

// Helper functions

func (tc *HardwareProfileWorkloadTestCtx) setupTestNamespace() {
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(hwpTestNamespace, map[string]string{"test-type": "hardware-profile"})),
		WithCustomErrorMsg("Failed to create hardware profile test namespace"),
	)
}

func (tc *HardwareProfileWorkloadTestCtx) cleanupTestNamespace() {
	tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: hwpTestNamespace}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
}

func (tc *HardwareProfileWorkloadTestCtx) createEmptyHardwareProfile(name string) *infrav1.HardwareProfile {
	hwp := &infrav1.HardwareProfile{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.HardwareProfile.Kind,
			APIVersion: infrav1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: hwpTestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(hwp),
		WithCustomErrorMsg("Failed to create the empty hardware profile"),
	)

	return hwp
}

func (tc *HardwareProfileWorkloadTestCtx) createHardwareProfileWithToleration(name string) *infrav1.HardwareProfile {
	hwp := &infrav1.HardwareProfile{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.HardwareProfile.Kind,
			APIVersion: infrav1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: hwpTestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  "CPU",
					Identifier:   "cpu",
					MinCount:     intstr.FromInt32(1),
					DefaultCount: intstr.FromInt32(2),
					ResourceType: "CPU",
				},
			},
			SchedulingSpec: &infrav1.SchedulingSpec{
				SchedulingType: infrav1.NodeScheduling,
				Node: &infrav1.NodeSchedulingSpec{
					NodeSelector: map[string]string{
						"kubernetes.io/os": "linux",
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "test-key",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(hwp),
		WithCustomErrorMsg("Failed to create hardware profile with toleration"),
	)

	return hwp
}

func (tc *HardwareProfileWorkloadTestCtx) createHardwareProfileWithKueue(name string) *infrav1.HardwareProfile {
	hwp := &infrav1.HardwareProfile{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.HardwareProfile.Kind,
			APIVersion: infrav1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: hwpTestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  "CPU",
					Identifier:   "cpu",
					MinCount:     intstr.FromInt32(2),
					DefaultCount: intstr.FromInt32(4),
					ResourceType: "CPU",
				},
			},
			SchedulingSpec: &infrav1.SchedulingSpec{
				SchedulingType: infrav1.QueueScheduling,
				Kueue: &infrav1.KueueSchedulingSpec{
					LocalQueueName: "default",
				},
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(hwp),
		WithCustomErrorMsg("Failed to create hardware profile with Kueue"),
	)

	return hwp
}

// createHardwareProfileWithCustomToleration creates a hardware profile with customizable
// toleration key and nodeSelector OS value. This is useful for testing profile switching
// scenarios where we need two profiles with distinct, identifiable tolerations.
func (tc *HardwareProfileWorkloadTestCtx) createHardwareProfileWithCustomToleration(name, tolerationKey, osValue string) *infrav1.HardwareProfile {
	hwp := &infrav1.HardwareProfile{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.HardwareProfile.Kind,
			APIVersion: infrav1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: hwpTestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  "CPU",
					Identifier:   "cpu",
					MinCount:     intstr.FromInt32(1),
					DefaultCount: intstr.FromInt32(2),
					ResourceType: "CPU",
				},
			},
			SchedulingSpec: &infrav1.SchedulingSpec{
				SchedulingType: infrav1.NodeScheduling,
				Node: &infrav1.NodeSchedulingSpec{
					NodeSelector: map[string]string{
						"kubernetes.io/os": osValue,
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      tolerationKey,
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(hwp),
		WithCustomErrorMsg("Failed to create hardware profile with custom toleration"),
	)

	return hwp
}
