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
