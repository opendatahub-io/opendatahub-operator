package upgrade_test

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	hardwareProfileKind  = "HardwareProfile"
	notebooksProfileType = "notebooks"
)

func TestCleanupDeprecatedKueueVAPB(t *testing.T) {
	ctx := t.Context()

	t.Run("should delete existing ValidatingAdmissionPolicyBinding during upgrade cleanup", func(t *testing.T) {
		g := NewWithT(t)

		// Create a deprecated ValidatingAdmissionPolicyBinding
		vapb := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kueue-validating-admission-policy-binding",
			},
		}

		// Create a DSCI to provide the application namespace
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		cli, err := fakeclient.New(fakeclient.WithObjects(vapb, dsci))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource which should trigger the Kueue VAPB cleanup
		oldRelease := common.Release{Version: version.OperatorVersion{Version: semver.MustParse("2.28.0")}}
		err = upgrade.CleanupExistingResource(ctx, cli, cluster.ManagedRhoai, oldRelease)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify that the ValidatingAdmissionPolicyBinding was deleted
		var deletedVAPB admissionregistrationv1.ValidatingAdmissionPolicyBinding
		err = cli.Get(ctx, client.ObjectKey{Name: "kueue-validating-admission-policy-binding"}, &deletedVAPB)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("not found"))
	})

	t.Run("should handle NotFound error gracefully during upgrade deletion", func(t *testing.T) {
		g := NewWithT(t)

		// Create a DSCI to provide the application namespace
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		interceptorFuncs := interceptor.Funcs{
			Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				return k8serr.NewNotFound(schema.GroupResource{
					Group:    "admissionregistration.k8s.io",
					Resource: "validatingadmissionpolicybindings",
				}, "kueue-validating-admission-policy-binding")
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource when the VAPB doesn't exist (NotFound error)
		oldRelease := common.Release{Version: version.OperatorVersion{Version: semver.MustParse("2.28.0")}}
		err = upgrade.CleanupExistingResource(ctx, cli, cluster.ManagedRhoai, oldRelease)
		g.Expect(err).ShouldNot(HaveOccurred(), "Should handle NotFound error gracefully")
	})

	t.Run("should handle NoMatch API error gracefully during upgrade deletion", func(t *testing.T) {
		g := NewWithT(t)

		// Create a DSCI to provide the application namespace
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		interceptorFuncs := interceptor.Funcs{
			Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				return &meta.NoKindMatchError{
					GroupKind: schema.GroupKind{
						Group: "admissionregistration.k8s.io",
						Kind:  "ValidatingAdmissionPolicyBinding",
					},
					SearchedVersions: []string{"v1beta1"},
				}
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource when the VAPB API v1beta1 is not available (NoMatch error)
		oldRelease := common.Release{Version: version.OperatorVersion{Version: semver.MustParse("2.28.0")}}
		err = upgrade.CleanupExistingResource(ctx, cli, cluster.ManagedRhoai, oldRelease)
		g.Expect(err).ShouldNot(HaveOccurred(), "Should handle NoMatch error gracefully")
	})
}

func TestMigrateAcceleratorProfilesToHardwareProfiles(t *testing.T) {
	ctx := t.Context()

	t.Run("should migrate AcceleratorProfiles to HardwareProfiles successfully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap := createTestAcceleratorProfile("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Should have 2 HWPs (notebooks and serving) for the AP
		notebooksHWP := findHardwareProfileByName(&hwpList, "test-ap-notebooks")
		servingHWP := findHardwareProfileByName(&hwpList, "test-ap-serving")

		g.Expect(notebooksHWP).ToNot(BeNil())
		g.Expect(servingHWP).ToNot(BeNil())

		// Validate notebooks HWP
		validateAcceleratorProfileHardwareProfile(g, notebooksHWP, ap, odhConfig, "notebooks")

		// Validate serving HWP
		validateAcceleratorProfileHardwareProfile(g, servingHWP, ap, odhConfig, "serving")
	})

	t.Run("should handle empty AcceleratorProfile list", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should handle AcceleratorProfile with tolerations", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithTolerations("test-namespace")
		ap := createTestAcceleratorProfileWithTolerations("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfile has tolerations
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		notebooksHWP := findHardwareProfileByName(&hwpList, "test-ap-notebooks")
		g.Expect(notebooksHWP).ToNot(BeNil())
		g.Expect(notebooksHWP.Spec.SchedulingSpec).ToNot(BeNil())
		g.Expect(notebooksHWP.Spec.SchedulingSpec.Node.Tolerations).ToNot(BeEmpty())
	})

	t.Run("should handle malformed AcceleratorProfile gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap := createMalformedAcceleratorProfile("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to generate"))
	})
}

func TestMigrateContainerSizesToHardwareProfiles(t *testing.T) {
	ctx := t.Context()

	t.Run("should migrate container sizes to HardwareProfiles successfully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateContainerSizesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfiles were created for container sizes
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		//
		notebooksSizeHWP := findHardwareProfileByName(&hwpList, "containerSize-small-notebooks")
		modelServerSizeHWP := findHardwareProfileByName(&hwpList, "containerSize-small-serving")

		g.Expect(notebooksSizeHWP).ToNot(BeNil())
		g.Expect(modelServerSizeHWP).ToNot(BeNil())

		// Get container size data from OdhDashboardConfig
		notebookSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "notebookSizes")
		modelServerSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "modelServerSizes")

		// Validate notebooks size HWP
		if len(notebookSizes) > 0 {
			containerSize, ok := notebookSizes[0].(map[string]interface{})
			if ok {
				validateContainerSizeHardwareProfile(g, notebooksSizeHWP, containerSize, odhConfig, "notebooks")
			}
		}

		// Validate model server size HWP
		if len(modelServerSizes) > 0 {
			containerSize, ok := modelServerSizes[0].(map[string]interface{})
			if ok {
				validateContainerSizeHardwareProfile(g, modelServerSizeHWP, containerSize, odhConfig, "serving")
			}
		}
	})

	t.Run("should handle missing container sizes gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithoutSizes("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateContainerSizesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should handle malformed container sizes gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithMalformedSizes("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateContainerSizesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred()) // Should skip malformed sizes
	})
}

func TestCreateSpecialHardwareProfile(t *testing.T) {
	ctx := t.Context()

	t.Run("should create special HardwareProfile successfully", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CreateSpecialHardwareProfile(ctx, cli, "test-namespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify special HardwareProfile was created
		var specialHWP infrav1.HardwareProfile
		err = cli.Get(ctx, client.ObjectKey{Name: "custom-serving", Namespace: "test-namespace"}, &specialHWP)
		g.Expect(err).ShouldNot(HaveOccurred())
		validateSpecialHardwareProfile(g, &specialHWP)
	})

	t.Run("should handle AlreadyExists error for special HardwareProfile gracefully", func(t *testing.T) {
		g := NewWithT(t)

		// Pre-create the special HardwareProfile
		existingHWP := &infrav1.HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-serving",
				Namespace: "test-namespace",
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(existingHWP))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CreateSpecialHardwareProfile(ctx, cli, "test-namespace")
		g.Expect(err).ShouldNot(HaveOccurred()) // Should handle AlreadyExists gracefully
	})

	t.Run("should handle creation errors for special HardwareProfile", func(t *testing.T) {
		g := NewWithT(t)

		interceptorFuncs := interceptor.Funcs{
			Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if obj.GetName() == "custom-serving" {
					return k8serr.NewInternalError(errors.New("failed to create special HWP"))
				}
				return nil
			},
		}

		cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptorFuncs))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CreateSpecialHardwareProfile(ctx, cli, "test-namespace")
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to create special HWP"))
	})
}

// Helper functions for creating test objects.
func createTestOdhDashboardConfig(namespace string) *unstructured.Unstructured {
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhConfig.SetName("odh-dashboard-config")
	odhConfig.SetNamespace(namespace)

	spec := map[string]interface{}{}
	spec["notebookSizes"] = []interface{}{
		map[string]interface{}{
			"name": "small",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    "1",
					"memory": "1Gi",
				},
				"limits": map[string]interface{}{
					"cpu":    "2",
					"memory": "2Gi",
				},
			},
		},
	}

	spec["modelServerSizes"] = []interface{}{
		map[string]interface{}{
			"name": "small",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    "1",
					"memory": "1Gi",
				},
				"limits": map[string]interface{}{
					"cpu":    "2",
					"memory": "2Gi",
				},
			},
		},
	}

	err := unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		panic(err)
	}
	return odhConfig
}

func createTestOdhDashboardConfigWithTolerations(namespace string) *unstructured.Unstructured {
	odhConfig := createTestOdhDashboardConfig(namespace)

	// Add notebook controller toleration settings
	notebookController := map[string]interface{}{
		"enabled": true,
		"notebookTolerationSettings": map[string]interface{}{
			"enabled":  true,
			"key":      "notebooks-only",
			"value":    "true",
			"operator": "Equal",
			"effect":   "NoSchedule",
		},
	}

	spec, _, _ := unstructured.NestedMap(odhConfig.Object, "spec")
	spec["notebookController"] = notebookController
	err := unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		panic(err)
	}

	return odhConfig
}

func createTestOdhDashboardConfigWithoutSizes(namespace string) *unstructured.Unstructured {
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhConfig.SetName("odh-dashboard-config")
	odhConfig.SetNamespace(namespace)

	// Set empty spec
	spec := map[string]interface{}{}
	err := unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		panic(err)
	}
	return odhConfig
}

func createTestOdhDashboardConfigWithMalformedSizes(namespace string) *unstructured.Unstructured {
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhConfig.SetName("odh-dashboard-config")
	odhConfig.SetNamespace(namespace)

	// Set spec with malformed container sizes
	spec := map[string]interface{}{
		"notebookSizes": []interface{}{
			map[string]interface{}{
				"name":      "malformed",
				"resources": "invalid", // Should be a map, not a string
			},
		},
	}

	err := unstructured.SetNestedMap(odhConfig.Object, spec, "spec")
	if err != nil {
		panic(err)
	}
	return odhConfig
}

func createTestAcceleratorProfile(namespace string) *unstructured.Unstructured {
	ap := &unstructured.Unstructured{}
	ap.SetGroupVersionKind(gvk.DashboardAcceleratorProfile)
	ap.SetName("test-ap")
	ap.SetNamespace(namespace)

	spec := map[string]interface{}{
		"identifier":  "nvidia.com/gpu",
		"displayName": "NVIDIA GPU",
		"description": "NVIDIA GPU accelerator",
		"enabled":     true,
	}

	err := unstructured.SetNestedMap(ap.Object, spec, "spec")
	if err != nil {
		panic(err)
	}
	return ap
}

func createTestAcceleratorProfileWithTolerations(namespace string) *unstructured.Unstructured {
	ap := createTestAcceleratorProfile(namespace)

	spec, _, _ := unstructured.NestedMap(ap.Object, "spec")
	tolerations := []interface{}{
		map[string]interface{}{
			"key":      "nvidia.com/gpu",
			"operator": "Equal",
			"value":    "true",
			"effect":   "NoSchedule",
		},
	}
	spec["tolerations"] = tolerations
	err := unstructured.SetNestedMap(ap.Object, spec, "spec")
	if err != nil {
		panic(err)
	}

	return ap
}

func createMalformedAcceleratorProfile(namespace string) *unstructured.Unstructured {
	ap := &unstructured.Unstructured{}
	ap.SetGroupVersionKind(gvk.DashboardAcceleratorProfile)
	ap.SetName("malformed-ap")
	ap.SetNamespace(namespace)

	// Missing spec field
	return ap
}

func findHardwareProfileByName(hwpList *infrav1.HardwareProfileList, name string) *infrav1.HardwareProfile {
	for i := range hwpList.Items {
		if hwpList.Items[i].Name == name {
			return &hwpList.Items[i]
		}
	}
	return nil
}

// validateAcceleratorProfileHardwareProfile validates that a HardwareProfile created from an AcceleratorProfile
// has the correct structure and values based on the input AcceleratorProfile and OdhDashboardConfig.
func validateAcceleratorProfileHardwareProfile(g *WithT, hwp *infrav1.HardwareProfile,
	ap *unstructured.Unstructured, odhConfig *unstructured.Unstructured, profileType string) {
	// Extract AP fields
	apName := ap.GetName()
	apNamespace := ap.GetNamespace()
	apSpec, _, _ := unstructured.NestedMap(ap.Object, "spec")
	identifier, _ := apSpec["identifier"].(string)
	displayName, _ := apSpec["displayName"].(string)
	description, _ := apSpec["description"].(string)
	enabled, _ := apSpec["enabled"].(bool)
	apAnnotations := ap.GetAnnotations()

	// Validate TypeMeta
	g.Expect(hwp.TypeMeta.APIVersion).To(Equal(infrav1.GroupVersion.String()))
	g.Expect(hwp.TypeMeta.Kind).To(Equal("HardwareProfile"))

	// Validate ObjectMeta
	g.Expect(hwp.GetName()).To(Equal(fmt.Sprintf("%s-%s", apName, profileType)))
	g.Expect(hwp.GetNamespace()).To(Equal(apNamespace))

	// Validate annotations
	annotations := hwp.GetAnnotations()
	g.Expect(annotations).ToNot(BeNil())
	g.Expect(annotations).To(HaveKey("opendatahub.io/dashboard-feature-visibility"))
	g.Expect(annotations["opendatahub.io/dashboard-feature-visibility"]).To(Equal(upgrade.GetFeatureVisibility(profileType)))
	g.Expect(annotations).To(HaveKey("opendatahub.io/modified-date"))
	g.Expect(annotations).To(HaveKey("opendatahub.io/display-name"))
	g.Expect(annotations["opendatahub.io/display-name"]).To(Equal(displayName))
	g.Expect(annotations).To(HaveKey("opendatahub.io/description"))
	g.Expect(annotations["opendatahub.io/description"]).To(Equal(description))
	g.Expect(annotations).To(HaveKey("opendatahub.io/disabled"))
	g.Expect(annotations["opendatahub.io/disabled"]).To(Equal(strconv.FormatBool(!enabled)))
	// All AP annotations should be present in HWP
	for k, v := range apAnnotations {
		g.Expect(annotations).To(HaveKeyWithValue(k, v))
	}

	// Validate identifiers
	g.Expect(hwp.Spec.Identifiers).To(HaveLen(3)) // Accelerator + CPU + Memory

	// Find accelerator, cpu, and memory identifiers
	var acceleratorIdentifier *infrav1.HardwareIdentifier
	var cpuIdentifier *infrav1.HardwareIdentifier
	var memoryIdentifier *infrav1.HardwareIdentifier

	for i := range hwp.Spec.Identifiers {
		id := &hwp.Spec.Identifiers[i]
		switch id.ResourceType {
		case "Accelerator":
			acceleratorIdentifier = id
		case "CPU":
			cpuIdentifier = id
		case "Memory":
			memoryIdentifier = id
		}
	}

	// Validate accelerator identifier
	g.Expect(acceleratorIdentifier).ToNot(BeNil())
	g.Expect(acceleratorIdentifier.Identifier).To(Equal(identifier))
	g.Expect(acceleratorIdentifier.DisplayName).To(Equal(identifier))
	g.Expect(acceleratorIdentifier.ResourceType).To(Equal("Accelerator"))
	g.Expect(acceleratorIdentifier.MinCount).To(Equal(intstr.FromInt(1)))
	g.Expect(acceleratorIdentifier.DefaultCount).To(Equal(intstr.FromInt(1)))

	// Validate CPU identifier
	g.Expect(cpuIdentifier).ToNot(BeNil())
	g.Expect(cpuIdentifier.Identifier).To(Equal("cpu"))
	g.Expect(cpuIdentifier.DisplayName).To(Equal("cpu"))
	g.Expect(cpuIdentifier.ResourceType).To(Equal("CPU"))
	// minCpu from containerCounts, which is calculated from odhConfig
	containerCounts, _ := upgrade.CalculateContainerResourceLimits(odhConfig, "notebookSizes")
	expectedMinCpu := intstr.FromString(containerCounts["minCpu"])
	g.Expect(cpuIdentifier.MinCount).To(Equal(expectedMinCpu))
	g.Expect(cpuIdentifier.DefaultCount).To(Equal(expectedMinCpu))
	if profileType == notebooksProfileType {
		if maxCpu, ok := containerCounts["maxCpu"]; ok && maxCpu != "" {
			g.Expect(cpuIdentifier.MaxCount).ToNot(BeNil())
			g.Expect(cpuIdentifier.MaxCount.Type).To(Equal(intstr.String))
			g.Expect(cpuIdentifier.MaxCount.StrVal).To(Equal(maxCpu))
		}
	}

	// Validate Memory identifier
	g.Expect(memoryIdentifier).ToNot(BeNil())
	g.Expect(memoryIdentifier.Identifier).To(Equal("memory"))
	g.Expect(memoryIdentifier.DisplayName).To(Equal("memory"))
	g.Expect(memoryIdentifier.ResourceType).To(Equal("Memory"))
	expectedMinMemory := intstr.FromString(containerCounts["minMemory"])
	g.Expect(memoryIdentifier.MinCount).To(Equal(expectedMinMemory))
	g.Expect(memoryIdentifier.DefaultCount).To(Equal(expectedMinMemory))
	if profileType == notebooksProfileType {
		if maxMemory, ok := containerCounts["maxMemory"]; ok && maxMemory != "" {
			g.Expect(memoryIdentifier.MaxCount).ToNot(BeNil())
			g.Expect(memoryIdentifier.MaxCount.Type).To(Equal(intstr.String))
			g.Expect(memoryIdentifier.MaxCount.StrVal).To(Equal(maxMemory))
		}
	}

	// Validate scheduling spec
	if hwp.Spec.SchedulingSpec != nil && len(hwp.Spec.SchedulingSpec.Node.Tolerations) > 0 {
		g.Expect(hwp.Spec.SchedulingSpec.SchedulingType).To(Equal(infrav1.NodeScheduling))
		g.Expect(hwp.Spec.SchedulingSpec.Node).ToNot(BeNil())
	}

	// Validate tolerations from AP
	apTolerations, _, _ := unstructured.NestedSlice(ap.Object, "spec", "tolerations")
	if hwp.Spec.SchedulingSpec != nil && hwp.Spec.SchedulingSpec.Node != nil {
		for _, apTol := range apTolerations {
			apTolMap, ok := apTol.(map[string]interface{})
			if !ok {
				continue
			}
			apTolKey, _ := apTolMap["key"].(string)
			apTolValue, _ := apTolMap["value"].(string)
			apTolOperator, _ := apTolMap["operator"].(string)
			apTolEffect, _ := apTolMap["effect"].(string)

			found := false
			for _, hwpTol := range hwp.Spec.SchedulingSpec.Node.Tolerations {
				if hwpTol.Key == apTolKey &&
					hwpTol.Value == apTolValue &&
					string(hwpTol.Operator) == apTolOperator &&
					string(hwpTol.Effect) == apTolEffect {
					found = true
					break
				}
			}
			g.Expect(found).To(BeTrue(), "AP toleration should be present in HWP")
		}
	}
	validateNotebooksOnlyToleration(g, hwp, odhConfig, profileType)
}

func validateNotebooksOnlyToleration(g *WithT, hwp *infrav1.HardwareProfile, odhConfig *unstructured.Unstructured, profileType string) {
	// Validate notebooks-only toleration for notebooks profile
	if profileType == notebooksProfileType && hwp.Spec.SchedulingSpec != nil && hwp.Spec.SchedulingSpec.Node != nil {
		notebookController, _, _ := unstructured.NestedMap(odhConfig.Object, "spec", "notebookController")
		if notebookController != nil {
			enabled, _, _ := unstructured.NestedBool(notebookController, "enabled")
			if enabled {
				tolerationSettings, _, _ := unstructured.NestedMap(notebookController, "notebookTolerationSettings")
				if tolerationSettings != nil {
					tolerationEnabled, _, _ := unstructured.NestedBool(tolerationSettings, "enabled")
					if tolerationEnabled {
						key, _, _ := unstructured.NestedString(tolerationSettings, "key")
						value, _, _ := unstructured.NestedString(tolerationSettings, "value")
						operator, _, _ := unstructured.NestedString(tolerationSettings, "operator")
						effect, _, _ := unstructured.NestedString(tolerationSettings, "effect")
						if key != "" {
							found := false
							for _, tol := range hwp.Spec.SchedulingSpec.Node.Tolerations {
								if tol.Key == key &&
									(value == "" || tol.Value == value) &&
									(operator == "" || string(tol.Operator) == operator) &&
									(effect == "" || string(tol.Effect) == effect) {
									found = true
									break
								}
							}
							g.Expect(found).To(BeTrue(), "Should have notebooks-only toleration")
						}
					}
				}
			}
		}
	}
}

// validateContainerSizeHardwareProfile validates that a HardwareProfile created from container sizes
// has the correct structure and values based on the input container size and OdhDashboardConfig.
func validateContainerSizeHardwareProfile(g *WithT, hwp *infrav1.HardwareProfile,
	containerSize map[string]interface{}, odhConfig *unstructured.Unstructured, sizeType string) {
	// Extract container size fields
	sizeName, _ := containerSize["name"].(string)
	resources, _ := containerSize["resources"].(map[string]interface{})
	requests, _ := resources["requests"].(map[string]interface{})
	limits, _ := resources["limits"].(map[string]interface{})

	// Validate TypeMeta
	g.Expect(hwp.TypeMeta.APIVersion).To(Equal(infrav1.GroupVersion.String()))
	g.Expect(hwp.TypeMeta.Kind).To(Equal("HardwareProfile"))

	// Validate ObjectMeta
	expectedName := fmt.Sprintf("containerSize-%s-%s", sizeName, sizeType)
	g.Expect(hwp.GetName()).To(Equal(expectedName))
	g.Expect(hwp.GetNamespace()).ToNot(BeEmpty()) // Should be set to application namespace

	// Validate annotations
	annotations := hwp.GetAnnotations()
	g.Expect(annotations).ToNot(BeNil())
	g.Expect(annotations).To(HaveKey("opendatahub.io/dashboard-feature-visibility"))
	g.Expect(annotations["opendatahub.io/dashboard-feature-visibility"]).To(Equal(upgrade.GetFeatureVisibility(sizeType)))
	g.Expect(annotations).To(HaveKey("opendatahub.io/modified-date"))
	g.Expect(annotations).To(HaveKey("opendatahub.io/display-name"))
	g.Expect(annotations["opendatahub.io/display-name"]).To(Equal(sizeName))
	g.Expect(annotations).To(HaveKey("opendatahub.io/description"))
	g.Expect(annotations["opendatahub.io/description"]).To(Equal(""))
	g.Expect(annotations).To(HaveKey("opendatahub.io/disabled"))
	g.Expect(annotations["opendatahub.io/disabled"]).To(Equal("false"))

	// Validate identifiers
	g.Expect(hwp.Spec.Identifiers).To(HaveLen(2)) // CPU + Memory

	// Find CPU and memory identifiers
	var cpuIdentifier *infrav1.HardwareIdentifier
	var memoryIdentifier *infrav1.HardwareIdentifier

	for i := range hwp.Spec.Identifiers {
		id := &hwp.Spec.Identifiers[i]
		switch id.ResourceType {
		case "CPU":
			cpuIdentifier = id
		case "Memory":
			memoryIdentifier = id
		}
	}

	// Validate CPU identifier
	g.Expect(cpuIdentifier).ToNot(BeNil())
	g.Expect(cpuIdentifier.Identifier).To(Equal("cpu"))
	g.Expect(cpuIdentifier.DisplayName).To(Equal("cpu"))
	g.Expect(cpuIdentifier.ResourceType).To(Equal("CPU"))

	// CPU MinCount and DefaultCount should be based on requests
	requestCpu, _ := requests["cpu"].(string)
	limitCpu, _ := limits["cpu"].(string)
	g.Expect(cpuIdentifier.MinCount).To(Equal(intstr.FromString(requestCpu)))
	g.Expect(cpuIdentifier.DefaultCount).To(Equal(intstr.FromString(requestCpu)))

	// CPU MaxCount should be based on limits
	if limitCpu != "" {
		g.Expect(cpuIdentifier.MaxCount).ToNot(BeNil())
		g.Expect(cpuIdentifier.MaxCount.Type).To(Equal(intstr.String))
		g.Expect(cpuIdentifier.MaxCount.StrVal).To(Equal(limitCpu))
	}

	// Validate Memory identifier
	g.Expect(memoryIdentifier).ToNot(BeNil())
	g.Expect(memoryIdentifier.Identifier).To(Equal("memory"))
	g.Expect(memoryIdentifier.DisplayName).To(Equal("memory"))
	g.Expect(memoryIdentifier.ResourceType).To(Equal("Memory"))

	// Memory MinCount and DefaultCount should be based on requests
	requestMemory, _ := requests["memory"].(string)
	limitMemory, _ := limits["memory"].(string)
	g.Expect(memoryIdentifier.MinCount).To(Equal(intstr.FromString(requestMemory)))
	g.Expect(memoryIdentifier.DefaultCount).To(Equal(intstr.FromString(requestMemory)))

	// Memory MaxCount should be based on limits
	if limitMemory != "" {
		g.Expect(memoryIdentifier.MaxCount).ToNot(BeNil())
		g.Expect(memoryIdentifier.MaxCount.Type).To(Equal(intstr.String))
		g.Expect(memoryIdentifier.MaxCount.StrVal).To(Equal(limitMemory))
	}

	// Validate scheduling spec
	if hwp.Spec.SchedulingSpec != nil {
		g.Expect(hwp.Spec.SchedulingSpec.SchedulingType).To(Equal(infrav1.NodeScheduling))
		g.Expect(hwp.Spec.SchedulingSpec.Node).ToNot(BeNil())
		g.Expect(hwp.Spec.SchedulingSpec.Node.Tolerations).ToNot(BeNil())
	}

	// Validate notebooks-only toleration for notebooks size type
	if sizeType == "notebooks" {
		validateNotebooksOnlyToleration(g, hwp, odhConfig, sizeType)
	}
}

// validateSpecialHardwareProfile validates that a Special HardwareProfile created
// has the correct structure and values based on the input OdhDashboardConfig.
func validateSpecialHardwareProfile(g *WithT, hwp *infrav1.HardwareProfile) {
	g.Expect(hwp).ToNot(BeNil())
	g.Expect(hwp.TypeMeta.APIVersion).To(Equal(infrav1.GroupVersion.String()))
	g.Expect(hwp.TypeMeta.Kind).To(Equal("HardwareProfile"))
	g.Expect(hwp.ObjectMeta.Name).To(Equal("custom-serving"))
	g.Expect(hwp.ObjectMeta.Annotations).ToNot(BeNil())
	g.Expect(hwp.ObjectMeta.Annotations["opendatahub.io/dashboard-feature-visibility"]).To(Equal("model-serving"))
	g.Expect(hwp.ObjectMeta.Annotations["opendatahub.io/display-name"]).To(Equal("custom-serving"))
	g.Expect(hwp.ObjectMeta.Annotations["opendatahub.io/description"]).To(Equal(""))
	g.Expect(hwp.ObjectMeta.Annotations["opendatahub.io/disabled"]).To(Equal("false"))
	// "opendatahub.io/modified-date" should be present and non-empty
	g.Expect(hwp.ObjectMeta.Annotations["opendatahub.io/modified-date"]).ToNot(BeEmpty())

	// Validate Spec.Identifiers
	g.Expect(hwp.Spec.Identifiers).To(HaveLen(2))
	cpuFound := false
	memFound := false
	for _, id := range hwp.Spec.Identifiers {
		switch id.Identifier {
		case "cpu":
			cpuFound = true
			g.Expect(id.DisplayName).To(Equal("cpu"))
			g.Expect(id.ResourceType).To(Equal("CPU"))
			g.Expect(id.MinCount).To(Equal(intstr.FromString("1")))
			g.Expect(id.DefaultCount).To(Equal(intstr.FromString("1")))
		case "memory":
			memFound = true
			g.Expect(id.DisplayName).To(Equal("memory"))
			g.Expect(id.ResourceType).To(Equal("Memory"))
			g.Expect(id.MinCount).To(Equal(intstr.FromString("1Gi")))
			g.Expect(id.DefaultCount).To(Equal(intstr.FromString("1Gi")))
		default:
			g.Expect(id.Identifier).To(BeElementOf("cpu", "memory"))
		}
	}
	g.Expect(cpuFound).To(BeTrue())
	g.Expect(memFound).To(BeTrue())
}

func TestHardwareProfileMigrationErrorAggregation(t *testing.T) {
	ctx := t.Context()

	t.Run("should aggregate multiple errors from different migration steps", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap := createTestAcceleratorProfile("test-namespace")

		// Create interceptor that fails both AcceleratorProfile and container size migrations
		interceptorFuncs := interceptor.Funcs{
			Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if obj.GetObjectKind().GroupVersionKind().Kind == hardwareProfileKind {
					// Fail creation for specific HWPs to test error aggregation
					if obj.GetName() == "test-ap-notebooks" || obj.GetName() == "containerSize-small-notebooks" {
						return k8serr.NewInternalError(errors.New("failed to create HardwareProfile"))
					}
				}
				return nil
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(odhConfig, ap),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "test-namespace")
		g.Expect(err).Should(HaveOccurred())

		// Should contain multiple error messages
		errStr := err.Error()
		g.Expect(errStr).To(ContainSubstring("failed to create"))
	})

	t.Run("should continue processing even when some steps fail", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap := createTestAcceleratorProfile("test-namespace")

		// Create interceptor that fails only AcceleratorProfile migration but allows others
		interceptorFuncs := interceptor.Funcs{
			Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if obj.GetObjectKind().GroupVersionKind().Kind == hardwareProfileKind {
					// Fail only AcceleratorProfile-based HWPs
					if obj.GetName() == "test-ap-notebooks" || obj.GetName() == "test-ap-serving" {
						return k8serr.NewInternalError(errors.New("failed to create AP HardwareProfile"))
					} else {
						return client.Create(ctx, obj, opts...)
					}
				}
				return nil
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(odhConfig, ap),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "test-namespace")
		g.Expect(err).Should(HaveOccurred())

		// Should still create other HardwareProfiles (container sizes and special)
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hwpList.Items).ToNot(BeEmpty()) // Should have some HWPs created
	})
}

func TestHardwareProfileMigrationWithComplexScenarios(t *testing.T) {
	ctx := t.Context()

	t.Run("should handle multiple AcceleratorProfiles with different configurations", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap1 := createTestAcceleratorProfile("test-namespace")
		ap1.SetName("gpu-ap")

		ap2 := createTestAcceleratorProfile("test-namespace")
		ap2.SetName("cpu-ap")
		// Modify the second AP to have different identifier
		spec, _, _ := unstructured.NestedMap(ap2.Object, "spec")
		spec["identifier"] = "cpu"
		err := unstructured.SetNestedMap(ap2.Object, spec, "spec")
		if err != nil {
			panic(err)
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap1, ap2))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "test-namespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify multiple HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hwpList.Items).To(HaveLen(7)) // 4 from 2 APs + 1 special + 2 container sizes

		// Find notebooks and serving HWPs
		gpuNotebooksHWP := findHardwareProfileByName(&hwpList, "gpu-ap-notebooks")
		gpuServingHWP := findHardwareProfileByName(&hwpList, "gpu-ap-serving")
		cpuNotebooksHWP := findHardwareProfileByName(&hwpList, "cpu-ap-notebooks")
		cpuServingHWP := findHardwareProfileByName(&hwpList, "cpu-ap-serving")

		g.Expect(gpuNotebooksHWP).ToNot(BeNil())
		g.Expect(gpuServingHWP).ToNot(BeNil())
		g.Expect(cpuNotebooksHWP).ToNot(BeNil())
		g.Expect(cpuServingHWP).ToNot(BeNil())

		// Validate notebooks HWP
		validateAcceleratorProfileHardwareProfile(g, gpuNotebooksHWP, ap1, odhConfig, "notebooks")
		validateAcceleratorProfileHardwareProfile(g, cpuNotebooksHWP, ap2, odhConfig, "notebooks")
		// Validate serving HWP
		validateAcceleratorProfileHardwareProfile(g, gpuServingHWP, ap1, odhConfig, "serving")
		validateAcceleratorProfileHardwareProfile(g, cpuServingHWP, ap2, odhConfig, "serving")

		notebooksSizeHWP := findHardwareProfileByName(&hwpList, "containerSize-small-notebooks")
		modelServerSizeHWP := findHardwareProfileByName(&hwpList, "containerSize-small-serving")
		g.Expect(notebooksSizeHWP).ToNot(BeNil())
		g.Expect(modelServerSizeHWP).ToNot(BeNil())

		// Get container size data from OdhDashboardConfig
		notebookSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "notebookSizes")
		modelServerSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "modelServerSizes")

		// Validate notebooks size HWP
		if len(notebookSizes) > 0 {
			containerSize, ok := notebookSizes[0].(map[string]interface{})
			if ok {
				validateContainerSizeHardwareProfile(g, notebooksSizeHWP, containerSize, odhConfig, "notebooks")
			}
		}

		// Validate model server size HWP
		if len(modelServerSizes) > 0 {
			containerSize, ok := modelServerSizes[0].(map[string]interface{})
			if ok {
				validateContainerSizeHardwareProfile(g, modelServerSizeHWP, containerSize, odhConfig, "serving")
			}
		}

		specialHWP := findHardwareProfileByName(&hwpList, "custom-serving")
		g.Expect(specialHWP).ToNot(BeNil())
		// Validate special HWP
		validateSpecialHardwareProfile(g, specialHWP)
	})

	t.Run("should handle AcceleratorProfile with complex tolerations", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithTolerations("test-namespace")
		ap := createTestAcceleratorProfileWithTolerations("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "test-namespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfile has both AP tolerations and notebooks-only tolerations
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		notebooksHWP := findHardwareProfileByName(&hwpList, "test-ap-notebooks")
		g.Expect(notebooksHWP).ToNot(BeNil())
		g.Expect(notebooksHWP.Spec.SchedulingSpec).ToNot(BeNil())
		g.Expect(len(notebooksHWP.Spec.SchedulingSpec.Node.Tolerations)).To(BeNumerically(">=", 2)) // AP + notebooks-only tolerations

		validateAcceleratorProfileHardwareProfile(g, notebooksHWP, ap, odhConfig, "notebooks")
	})

	t.Run("should handle partial failures gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap := createTestAcceleratorProfile("test-namespace")

		// Pre-create one HardwareProfile to simulate AlreadyExists
		existingHWP := &infrav1.HardwareProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ap-notebooks",
				Namespace: "test-namespace",
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap, existingHWP))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "test-namespace")
		g.Expect(err).ShouldNot(HaveOccurred()) // Should handle AlreadyExists gracefully

		// Verify other HardwareProfiles were still created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(len(hwpList.Items)).To(BeNumerically(">", 1)) // Should have more than just the existing one
	})
}
