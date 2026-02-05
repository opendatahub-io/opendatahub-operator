package upgrade_test

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// TestMigrateToInfraHardwareProfilesIdempotence verifies the idempotence of HardwareProfile migration.
//
// Idempotence Definition:
// The migration function can be run multiple times without:
//   - Producing errors (e.g., AlreadyExists on second run)
//   - Creating duplicate resources
//   - Changing the final cluster state (except timestamps)
//
// Test Strategy:
// Each test scenario runs the migration 2-3 times and compares cluster state snapshots to ensure
// they remain identical. The test captures HardwareProfiles, Notebook annotations, and ISVC annotations
// before and after each run, then performs deep comparison ignoring timestamp fields.
//
// Test Scenarios:
//  1. Full migration - Clean state with AcceleratorProfiles, container sizes, and workloads
//  2. Partial completion - Some HardwareProfiles already exist
//  3. Partial annotations - Some resources already have HWP annotations
//  4. Already migrated - All resources fully migrated
//  5. Concurrent changes - New resources added between migration runs
//  6. Mixed state - Combination of migrated and non-migrated resources
//
// Implementation Note:
// This test calls MigrateToInfraHardwareProfiles directly instead of CleanupExistingResource
// because the fake client test environment doesn't include dashboard types (AcceleratorProfile,
// OdhDashboardConfig, etc.) in its scheme, causing HasCRD checks to fail even when CRD objects
// are created. Testing the migration function directly provides better coverage of the actual
// migration logic while bypassing the CRD detection mechanism.

func TestMigrateToInfraHardwareProfilesIdempotence_FullMigration(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	namespace := "test-idempotence-ns"

	// Create OdhDashboardConfig
	odhConfig := createTestOdhDashboardConfig(t, namespace)

	// Create AcceleratorProfiles
	ap1 := createTestAcceleratorProfile(t, namespace)
	ap1.SetName("gpu-profile")

	ap2 := createTestAcceleratorProfile(t, namespace)
	ap2.SetName("tpu-profile")
	spec2, found, err := unstructured.NestedMap(ap2.Object, "spec")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get spec from AcceleratorProfile")
	g.Expect(found).To(BeTrue(), "spec not found in AcceleratorProfile")

	spec2["identifier"] = "google.com/tpu"
	spec2["displayName"] = "TPU"
	err = unstructured.SetNestedMap(ap2.Object, spec2, "spec")
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create Notebooks with various annotations
	nb1 := createTestNotebook(namespace, "notebook-ap")
	nb1.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name": "gpu-profile",
	})

	nb2 := createTestNotebook(namespace, "notebook-size")
	nb2.SetAnnotations(map[string]string{
		"notebooks.opendatahub.io/last-size-selection": "Small",
	})

	nb3 := createTestNotebook(namespace, "notebook-no-annotation")

	// Create InferenceServices
	servingRuntime := createTestServingRuntime(namespace, "runtime-with-ap")
	servingRuntime.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name": "tpu-profile",
	})

	isvc1 := createTestInferenceService(namespace, "isvc-with-runtime", "runtime-with-ap")
	isvc2 := createTestInferenceServiceWithResources(namespace, "isvc-with-matching-size", "1", "4Gi", "2", "8Gi")
	isvc3 := createTestInferenceServiceWithResources(namespace, "isvc-custom", "3", "10Gi", "5", "20Gi")

	// Create all objects
	cli, err := fakeclient.New(fakeclient.WithObjects(
		odhConfig, ap1, ap2,
		nb1, nb2, nb3, servingRuntime, isvc1, isvc2, isvc3,
	))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run of migration
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Capture state after first run
	stateAfterFirstRun, err := captureClusterState(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify expected state after first run
	g.Expect(stateAfterFirstRun.HardwareProfiles).NotTo(BeEmpty(), "HardwareProfiles should be created")

	// Verify notebook annotations were set
	nb1Annotations := stateAfterFirstRun.NotebookAnnotations["notebook-ap"]
	g.Expect(nb1Annotations).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "gpu-profile-notebooks"))

	nb2Annotations := stateAfterFirstRun.NotebookAnnotations["notebook-size"]
	g.Expect(nb2Annotations).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-small-notebooks"))

	// Verify InferenceService annotations were set
	isvc1Annotations := stateAfterFirstRun.ISVCAnnotations["isvc-with-runtime"]
	g.Expect(isvc1Annotations).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "tpu-profile-serving"))

	isvc2Annotations := stateAfterFirstRun.ISVCAnnotations["isvc-with-matching-size"]
	g.Expect(isvc2Annotations).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-small-serving"))

	isvc3Annotations := stateAfterFirstRun.ISVCAnnotations["isvc-custom"]
	g.Expect(isvc3Annotations).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "custom-serving"))

	// Verify idempotence with 2 additional runs
	verifyClusterStateIdempotence(ctx, g, cli, namespace, 2, stateAfterFirstRun)
}

func TestMigrateToInfraHardwareProfilesIdempotence_PartialCompletionSomeHWPsExist(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	namespace := "test-partial-ns"

	odhConfig := createTestOdhDashboardConfig(t, namespace)
	ap := createTestAcceleratorProfile(t, namespace)

	// Pre-create one of the HardwareProfiles that would be created
	existingHWP := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ap-notebooks",
			Namespace: namespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					Identifier:   "cpu",
					ResourceType: "CPU",
					MinCount:     intstr.FromInt(1),
					DefaultCount: intstr.FromInt(1),
				},
			},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap, existingHWP))
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify idempotence - first run handles existing HWP, subsequent runs are no-op
	verifyClusterStateIdempotence(ctx, g, cli, namespace, 2, nil)
}

func TestMigrateToInfraHardwareProfilesIdempotence_PreservesUserModifications(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	namespace := "test-preserve-modifications-ns"

	odhConfig := createTestOdhDashboardConfig(t, namespace)
	ap := createTestAcceleratorProfile(t, namespace)

	// Pre-create HWP with custom user modifications
	// This simulates a user who has customized the HWP after initial migration
	userCustomizedHWP := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ap-notebooks",
			Namespace: namespace,
			Annotations: map[string]string{
				"user-custom-annotation": "my-custom-value",
				"user-added-label":       "important",
			},
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					Identifier:   "cpu",
					DisplayName:  "Custom CPU Name", // User customized
					ResourceType: "CPU",
					MinCount:     intstr.FromInt(999),   // User customized
					DefaultCount: intstr.FromInt(999),   // User customized
					MaxCount:     &intstr.IntOrString{}, // User customized
				},
				{
					Identifier:   "memory",
					DisplayName:  "Custom Memory", // User customized
					ResourceType: "Memory",
					MinCount:     intstr.FromString("999Gi"), // User customized
					DefaultCount: intstr.FromString("999Gi"), // User customized
				},
				{
					Identifier:   "nvidia.com/gpu",
					DisplayName:  "nvidia.com/gpu",
					ResourceType: "Accelerator",
					MinCount:     intstr.FromInt(1),
					DefaultCount: intstr.FromInt(1),
				},
			},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap, userCustomizedHWP))
	g.Expect(err).ShouldNot(HaveOccurred())

	// Capture user's custom spec before migration
	var hwpBeforeMigration infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: "test-ap-notebooks", Namespace: namespace}, &hwpBeforeMigration)
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run of migration - should NOT overwrite user's HWP
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify user's custom spec was preserved
	var hwpAfterFirstRun infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: "test-ap-notebooks", Namespace: namespace}, &hwpAfterFirstRun)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify spec is identical to user's version (not overwritten)
	g.Expect(reflect.DeepEqual(hwpBeforeMigration.Spec, hwpAfterFirstRun.Spec)).To(BeTrue(),
		"User's custom HWP spec should be preserved, not overwritten")

	// Verify user's custom annotations are preserved
	g.Expect(hwpAfterFirstRun.Annotations).To(HaveKeyWithValue("user-custom-annotation", "my-custom-value"))
	g.Expect(hwpAfterFirstRun.Annotations).To(HaveKeyWithValue("user-added-label", "important"))

	// Second run - verify spec still unchanged (idempotent)
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	var hwpAfterSecondRun infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: "test-ap-notebooks", Namespace: namespace}, &hwpAfterSecondRun)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify spec remains identical across multiple runs
	g.Expect(reflect.DeepEqual(hwpAfterFirstRun.Spec, hwpAfterSecondRun.Spec)).To(BeTrue(),
		"User's custom HWP spec should remain unchanged across multiple migration runs")
}

func TestMigrateToInfraHardwareProfilesIdempotence_PartialAnnotations(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	namespace := "test-partial-annotations-ns"

	odhConfig := createTestOdhDashboardConfig(t, namespace)
	ap := createTestAcceleratorProfile(t, namespace)

	// Create notebooks - one already has HWP annotation, one doesn't
	nb1 := createTestNotebook(namespace, "notebook-already-migrated")
	nb1.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name":           "test-ap",
		"opendatahub.io/hardware-profile-name":      "test-ap-notebooks",
		"opendatahub.io/hardware-profile-namespace": namespace,
	})

	nb2 := createTestNotebook(namespace, "notebook-needs-migration")
	nb2.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name": "test-ap",
	})

	cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap, nb1, nb2))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureClusterState(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify both notebooks have HWP annotation now
	g.Expect(stateAfterFirstRun.NotebookAnnotations["notebook-already-migrated"]).To(
		HaveKeyWithValue("opendatahub.io/hardware-profile-name", "test-ap-notebooks"))
	g.Expect(stateAfterFirstRun.NotebookAnnotations["notebook-needs-migration"]).To(
		HaveKeyWithValue("opendatahub.io/hardware-profile-name", "test-ap-notebooks"))

	// Verify idempotence with 1 additional run
	verifyClusterStateIdempotence(ctx, g, cli, namespace, 1, stateAfterFirstRun)
}

func TestMigrateToInfraHardwareProfilesIdempotence_AllAlreadyMigrated(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	namespace := "test-all-migrated-ns"

	odhConfig := createTestOdhDashboardConfig(t, namespace)

	// Create notebook that already has HWP annotation
	nb := createTestNotebook(namespace, "already-migrated-notebook")
	nb.SetAnnotations(map[string]string{
		"opendatahub.io/hardware-profile-name":      "some-hwp",
		"opendatahub.io/hardware-profile-namespace": namespace,
	})

	// Create InferenceService that already has HWP annotation
	isvc := createTestInferenceService(namespace, "already-migrated-isvc", "")
	isvc.SetAnnotations(map[string]string{
		"opendatahub.io/hardware-profile-name":      "custom-serving",
		"opendatahub.io/hardware-profile-namespace": namespace,
	})

	// All HWPs already exist
	hwp1 := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-hwp",
			Namespace: namespace,
		},
	}

	hwp2 := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-serving",
			Namespace: namespace,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, nb, isvc, hwp1, hwp2))
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify idempotence - all resources already migrated
	verifyClusterStateIdempotence(ctx, g, cli, namespace, 2, nil)
}

func TestMigrateToInfraHardwareProfilesIdempotence_ConcurrentChanges(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	namespace := "test-concurrent-ns"

	odhConfig := createTestOdhDashboardConfig(t, namespace)
	ap1 := createTestAcceleratorProfile(t, namespace)
	ap1.SetName("initial-ap")

	nb1 := createTestNotebook(namespace, "initial-notebook")
	nb1.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name": "initial-ap",
	})

	cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap1, nb1))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureClusterState(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Simulate new resources being added (new AcceleratorProfile and Notebook)
	ap2 := createTestAcceleratorProfile(t, namespace)
	ap2.SetName("new-ap")
	spec, found, err := unstructured.NestedMap(ap2.Object, "spec")
	g.Expect(err).ShouldNot(HaveOccurred(), "failed to get spec from AcceleratorProfile")
	g.Expect(found).To(BeTrue(), "spec not found in AcceleratorProfile")

	spec["identifier"] = "new.com/accelerator"
	err = unstructured.SetNestedMap(ap2.Object, spec, "spec")
	g.Expect(err).ShouldNot(HaveOccurred())
	err = cli.Create(ctx, ap2)
	g.Expect(err).ShouldNot(HaveOccurred())

	nb2 := createTestNotebook(namespace, "new-notebook")
	nb2.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name": "new-ap",
	})
	err = cli.Create(ctx, nb2)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Second run - should handle new resources
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterSecondRun, err := captureClusterState(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify new HWPs were created
	g.Expect(len(stateAfterSecondRun.HardwareProfiles)).To(BeNumerically(">", len(stateAfterFirstRun.HardwareProfiles)))

	// Verify new notebook got annotation
	g.Expect(stateAfterSecondRun.NotebookAnnotations["new-notebook"]).To(
		HaveKeyWithValue("opendatahub.io/hardware-profile-name", "new-ap-notebooks"))

	// Verify idempotence with 1 additional run
	verifyClusterStateIdempotence(ctx, g, cli, namespace, 1, stateAfterSecondRun)
}

func TestMigrateToInfraHardwareProfilesIdempotence_MixedState(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)
	namespace := "test-mixed-state-ns"

	odhConfig := createTestOdhDashboardConfig(t, namespace)
	ap := createTestAcceleratorProfile(t, namespace)

	// Create mix of notebooks
	nb1 := createTestNotebook(namespace, "nb-already-has-hwp")
	nb1.SetAnnotations(map[string]string{
		"opendatahub.io/hardware-profile-name": "test-ap-notebooks",
	})

	nb2 := createTestNotebook(namespace, "nb-has-ap-annotation")
	nb2.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name": "test-ap",
	})

	nb3 := createTestNotebook(namespace, "nb-has-size-annotation")
	nb3.SetAnnotations(map[string]string{
		"notebooks.opendatahub.io/last-size-selection": "Small",
	})

	nb4 := createTestNotebook(namespace, "nb-no-annotations")

	// Create mix of InferenceServices
	servingRuntime := createTestServingRuntime(namespace, "runtime-ap")
	servingRuntime.SetAnnotations(map[string]string{
		"opendatahub.io/accelerator-name": "test-ap",
	})

	isvc1 := createTestInferenceService(namespace, "isvc-already-migrated", "")
	isvc1.SetAnnotations(map[string]string{
		"opendatahub.io/hardware-profile-name": "custom-serving",
	})

	isvc2 := createTestInferenceService(namespace, "isvc-with-runtime", "runtime-ap")

	// Pre-create some HWPs
	hwp1 := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ap-notebooks",
			Namespace: namespace,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(
		odhConfig, ap, nb1, nb2, nb3, nb4,
		servingRuntime, isvc1, isvc2, hwp1,
	))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureClusterState(ctx, cli, namespace)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify all notebooks now have HWP annotations
	g.Expect(stateAfterFirstRun.NotebookAnnotations["nb-already-has-hwp"]).To(
		HaveKey("opendatahub.io/hardware-profile-name"))
	g.Expect(stateAfterFirstRun.NotebookAnnotations["nb-has-ap-annotation"]).To(
		HaveKeyWithValue("opendatahub.io/hardware-profile-name", "test-ap-notebooks"))
	g.Expect(stateAfterFirstRun.NotebookAnnotations["nb-has-size-annotation"]).To(
		HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-small-notebooks"))

	// nb-no-annotations should not have HWP annotation (no source to migrate from)
	_, hasHWP := stateAfterFirstRun.NotebookAnnotations["nb-no-annotations"]["opendatahub.io/hardware-profile-name"]
	g.Expect(hasHWP).To(BeFalse())

	// Verify idempotence with 2 additional runs
	verifyClusterStateIdempotence(ctx, g, cli, namespace, 2, stateAfterFirstRun)
}

func TestMigrateToInfraHardwareProfilesIdempotence_EmptyNamespace(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	odhConfig := createTestOdhDashboardConfig(t, "test-namespace")
	ap := createTestAcceleratorProfile(t, "test-namespace")

	cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run with empty namespace - should skip migration
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "")
	g.Expect(err).ShouldNot(HaveOccurred())

	// Capture state after first run
	stateAfterFirstRun, err := captureClusterState(ctx, cli, "test-namespace")
	g.Expect(err).ShouldNot(HaveOccurred())

	// Should have no HardwareProfiles created
	g.Expect(stateAfterFirstRun.HardwareProfiles).To(BeEmpty(), "No HardwareProfiles should be created when namespace is empty")

	// Second run with empty namespace - should still skip migration
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "")
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterSecondRun, err := captureClusterState(ctx, cli, "test-namespace")
	g.Expect(err).ShouldNot(HaveOccurred())

	// States should be identical (both empty)
	identical, differences := compareClusterStates(stateAfterFirstRun, stateAfterSecondRun)
	g.Expect(identical).To(BeTrue(), "States should be identical when namespace is empty. Differences: %v", differences)

	// Third run to be extra sure
	err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "")
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterThirdRun, err := captureClusterState(ctx, cli, "test-namespace")
	g.Expect(err).ShouldNot(HaveOccurred())

	identical, differences = compareClusterStates(stateAfterSecondRun, stateAfterThirdRun)
	g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
}
