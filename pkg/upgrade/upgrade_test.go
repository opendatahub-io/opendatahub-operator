package upgrade_test

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig)
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

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should handle AcceleratorProfile with tolerations", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithTolerations("test-namespace")
		ap := createTestAcceleratorProfileWithTolerations("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig)
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

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig)
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

		// Get container size data from OdhDashboardConfig
		notebookSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "notebookSizes")
		modelServerSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "modelServerSizes")

		// Validate notebooks size HWP
		if len(notebookSizes) > 0 {
			for _, nb := range notebookSizes {
				containerSize, ok := nb.(map[string]interface{})
				if ok {
					name, _ := containerSize["name"].(string)
					hwpName := strings.ReplaceAll(strings.ToLower(fmt.Sprintf("containerSize-%s-notebooks", name)), " ", "-")
					notebooksSizeHWP := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(notebooksSizeHWP).ToNot(BeNil())
					validateContainerSizeHardwareProfile(g, notebooksSizeHWP, containerSize, odhConfig, "notebooks")
				}
			}
		}

		// Validate model server size HWP
		if len(modelServerSizes) > 0 {
			for _, ms := range modelServerSizes {
				containerSize, ok := ms.(map[string]interface{})
				if ok {
					name, _ := containerSize["name"].(string)
					hwpName := strings.ReplaceAll(strings.ToLower(fmt.Sprintf("containerSize-%s-serving", name)), " ", "-")
					modelServerSizeHWP := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(modelServerSizeHWP).ToNot(BeNil())
					validateContainerSizeHardwareProfile(g, modelServerSizeHWP, containerSize, odhConfig, "serving")
				}
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

// Helper functions for creating test objects.
func createTestOdhDashboardConfig(namespace string) *unstructured.Unstructured {
	odhConfig := &unstructured.Unstructured{}
	odhConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhConfig.SetName("odh-dashboard-config")
	odhConfig.SetNamespace(namespace)

	spec := map[string]interface{}{}
	spec["notebookSizes"] = []interface{}{
		map[string]interface{}{
			"name": "Small",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "8Gi",
					"cpu":    "1",
				},
				"limits": map[string]interface{}{
					"memory": "8Gi",
					"cpu":    "2",
				},
			},
		},
		map[string]interface{}{
			"name": "Medium",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "24Gi",
					"cpu":    "3",
				},
				"limits": map[string]interface{}{
					"memory": "24Gi",
					"cpu":    "6",
				},
			},
		},
		map[string]interface{}{
			"name": "X Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "120Gi",
					"cpu":    "15",
				},
				"limits": map[string]interface{}{
					"memory": "120Gi",
					"cpu":    "30",
				},
			},
		},
		map[string]interface{}{
			"name": "Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"memory": "56Gi",
					"cpu":    "7",
				},
				"limits": map[string]interface{}{
					"memory": "56Gi",
					"cpu":    "14",
				},
			},
		},
	}

	spec["modelServerSizes"] = []interface{}{
		map[string]interface{}{
			"name": "Large",
			"resources": map[string]interface{}{
				"limits": map[string]interface{}{
					"cpu":    "10",
					"memory": "20Gi",
				},
				"requests": map[string]interface{}{
					"cpu":    "6",
					"memory": "16Gi",
				},
			},
		},
		map[string]interface{}{
			"name": "Small",
			"resources": map[string]interface{}{
				"limits": map[string]interface{}{
					"cpu":    "2",
					"memory": "8Gi",
				},
				"requests": map[string]interface{}{
					"cpu":    "1",
					"memory": "4Gi",
				},
			},
		},
		map[string]interface{}{
			"name": "Medium",
			"resources": map[string]interface{}{
				"limits": map[string]interface{}{
					"cpu":    "8",
					"memory": "10Gi",
				},
				"requests": map[string]interface{}{
					"cpu":    "4",
					"memory": "8Gi",
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
	expectedVisibility := `["model-serving"]`
	if profileType == notebooksProfileType {
		expectedVisibility = `["workbench"]`
	}
	g.Expect(annotations["opendatahub.io/dashboard-feature-visibility"]).To(Equal(expectedVisibility))
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

	// Validate Memory identifier
	g.Expect(memoryIdentifier).ToNot(BeNil())
	g.Expect(memoryIdentifier.Identifier).To(Equal("memory"))
	g.Expect(memoryIdentifier.DisplayName).To(Equal("memory"))
	g.Expect(memoryIdentifier.ResourceType).To(Equal("Memory"))

	if profileType == notebooksProfileType {
		// containerCounts, which is calculated from odhConfig
		containerCounts, _ := upgrade.FindContainerCpuMemoryMinMaxCount(odhConfig, "notebookSizes")

		// minCpu
		expectedMinCpu := intstr.FromString(containerCounts["minCpu"])
		g.Expect(cpuIdentifier.MinCount).To(Equal(expectedMinCpu))
		g.Expect(cpuIdentifier.DefaultCount).To(Equal(expectedMinCpu))

		// maxCpu
		if maxCpu, ok := containerCounts["maxCpu"]; ok && maxCpu != "" {
			g.Expect(cpuIdentifier.MaxCount).ToNot(BeNil())
			g.Expect(cpuIdentifier.MaxCount.Type).To(Equal(intstr.String))
			g.Expect(cpuIdentifier.MaxCount.StrVal).To(Equal(maxCpu))
		}

		// minMemory
		expectedMinMemory := intstr.FromString(containerCounts["minMemory"])
		g.Expect(memoryIdentifier.MinCount).To(Equal(expectedMinMemory))
		g.Expect(memoryIdentifier.DefaultCount).To(Equal(expectedMinMemory))

		// maxMemory
		if maxMemory, ok := containerCounts["maxMemory"]; ok && maxMemory != "" {
			g.Expect(memoryIdentifier.MaxCount).ToNot(BeNil())
			g.Expect(memoryIdentifier.MaxCount.Type).To(Equal(intstr.String))
			g.Expect(memoryIdentifier.MaxCount.StrVal).To(Equal(maxMemory))
		}
	} else {
		minCpu := intstr.FromString("1")
		g.Expect(cpuIdentifier.MinCount).To(Equal(minCpu))
		g.Expect(cpuIdentifier.DefaultCount).To(Equal(minCpu))
		minMem := intstr.FromString("1Gi")
		g.Expect(memoryIdentifier.MinCount).To(Equal(minMem))
		g.Expect(memoryIdentifier.DefaultCount).To(Equal(minMem))
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
	expectedName = strings.ReplaceAll(strings.ToLower(expectedName), " ", "-")
	g.Expect(hwp.GetName()).To(Equal(expectedName))
	g.Expect(hwp.GetNamespace()).ToNot(BeEmpty()) // Should be set to application namespace

	// Validate annotations
	annotations := hwp.GetAnnotations()
	g.Expect(annotations).ToNot(BeNil())
	g.Expect(annotations).To(HaveKey("opendatahub.io/dashboard-feature-visibility"))
	expectedVisibility := `["model-serving"]`
	if sizeType == notebooksProfileType {
		expectedVisibility = `["workbench"]`
	}
	g.Expect(annotations["opendatahub.io/dashboard-feature-visibility"]).To(Equal(expectedVisibility))
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

		// Get container size data from OdhDashboardConfig
		notebookSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "notebookSizes")
		modelServerSizes, _, _ := unstructured.NestedSlice(odhConfig.Object, "spec", "modelServerSizes")

		if len(notebookSizes) > 0 {
			for _, nb := range notebookSizes {
				containerSize, ok := nb.(map[string]interface{})
				if ok {
					name, _ := containerSize["name"].(string)
					hwpName := strings.ReplaceAll(strings.ToLower(fmt.Sprintf("containerSize-%s-notebooks", name)), " ", "-")
					notebooksSizeHWP := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(notebooksSizeHWP).ToNot(BeNil())
					validateContainerSizeHardwareProfile(g, notebooksSizeHWP, containerSize, odhConfig, "notebooks")
				}
			}
		}

		// Validate model server size HWP
		if len(modelServerSizes) > 0 {
			for _, ms := range modelServerSizes {
				containerSize, ok := ms.(map[string]interface{})
				if ok {
					name, _ := containerSize["name"].(string)
					hwpName := strings.ReplaceAll(strings.ToLower(fmt.Sprintf("containerSize-%s-serving", name)), " ", "-")
					modelServerSizeHWP := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(modelServerSizeHWP).ToNot(BeNil())
					validateContainerSizeHardwareProfile(g, modelServerSizeHWP, containerSize, odhConfig, "serving")
				}
			}
		}
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
	t.Run("should skip migration if application namespace is empty", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap := createTestAcceleratorProfile("test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify no HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hwpList.Items).To(BeEmpty())
	})
}

func TestFindCpuMemoryMinMaxCountFromContainerSizes(t *testing.T) {
	tests := []struct {
		name           string
		containerSizes []upgrade.ContainerSize
		want           map[string]string
		wantErr        bool
	}{
		{
			name: "single size",
			containerSizes: []upgrade.ContainerSize{
				{
					Name: "Small",
					Resources: struct {
						Requests struct {
							Cpu    string
							Memory string
						}
						Limits struct {
							Cpu    string
							Memory string
						}
					}{
						Requests: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "1",
							Memory: "1Gi",
						},
						Limits: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "1",
							Memory: "1Gi",
						},
					},
				},
			},
			want: map[string]string{
				"minCpu":    "1",
				"minMemory": "1Gi",
				"maxCpu":    "1",
				"maxMemory": "1Gi",
			},
			wantErr: false,
		},
		{
			name: "multiple sizes",
			containerSizes: []upgrade.ContainerSize{
				{
					Name: "Small",
					Resources: struct {
						Requests struct {
							Cpu    string
							Memory string
						}
						Limits struct {
							Cpu    string
							Memory string
						}
					}{
						Requests: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "1",
							Memory: "1Gi",
						},
						Limits: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "2",
							Memory: "2Gi",
						},
					},
				},
				{
					Name: "Large",
					Resources: struct {
						Requests struct {
							Cpu    string
							Memory string
						}
						Limits struct {
							Cpu    string
							Memory string
						}
					}{
						Requests: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "4",
							Memory: "8Gi",
						},
						Limits: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "8",
							Memory: "16Gi",
						},
					},
				},
			},
			want: map[string]string{
				"minCpu":    "1",
				"minMemory": "1Gi",
				"maxCpu":    "8",
				"maxMemory": "16Gi",
			},
			wantErr: false,
		},
		{
			name: "multiple size different order",
			containerSizes: []upgrade.ContainerSize{
				{
					Name: "Large",
					Resources: struct {
						Requests struct {
							Cpu    string
							Memory string
						}
						Limits struct {
							Cpu    string
							Memory string
						}
					}{
						Requests: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "4",
							Memory: "8Gi",
						},
						Limits: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "8",
							Memory: "16Gi",
						},
					},
				},
				{
					Name: "Small",
					Resources: struct {
						Requests struct {
							Cpu    string
							Memory string
						}
						Limits struct {
							Cpu    string
							Memory string
						}
					}{
						Requests: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "1",
							Memory: "1Gi",
						},
						Limits: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "2",
							Memory: "2Gi",
						},
					},
				},
				{
					Name: "Medium",
					Resources: struct {
						Requests struct {
							Cpu    string
							Memory string
						}
						Limits struct {
							Cpu    string
							Memory string
						}
					}{
						Requests: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "3",
							Memory: "3Gi",
						},
						Limits: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "6",
							Memory: "6Gi",
						},
					},
				},
			},
			want: map[string]string{
				"minCpu":    "1",
				"minMemory": "1Gi",
				"maxCpu":    "8",
				"maxMemory": "16Gi",
			},
			wantErr: false,
		},
		{
			name:           "empty sizes",
			containerSizes: []upgrade.ContainerSize{},
			want:           map[string]string{"minMemory": "1Mi", "minCpu": "1"},
			wantErr:        false,
		},
		{
			name: "malformed size (bad cpu)",
			containerSizes: []upgrade.ContainerSize{
				{
					Name: "bad",
					Resources: struct {
						Requests struct {
							Cpu    string
							Memory string
						}
						Limits struct {
							Cpu    string
							Memory string
						}
					}{
						Requests: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "not-a-number",
							Memory: "1Gi",
						},
						Limits: struct {
							Cpu    string
							Memory string
						}{
							Cpu:    "2",
							Memory: "2Gi",
						},
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			counts, err := upgrade.FindCpuMemoryMinMaxCountFromContainerSizes(tt.containerSizes)
			if tt.wantErr {
				g.Expect(err).Should(HaveOccurred())
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
				for k, v := range tt.want {
					g.Expect(counts).To(HaveKeyWithValue(k, v))
				}
			}
		})
	}
}

func TestAttachHardwareProfileToNotebooks(t *testing.T) {
	ctx := context.Background()
	namespace := "test-namespace"

	t.Run("should migrate AP annotation to HWP annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		notebook := createTestNotebook(namespace, "notebook-with-ap")
		notebook.SetAnnotations(map[string]string{
			"opendatahub.io/accelerator-name": "nvidia-gpu",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, notebook))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation was added
		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-with-ap", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "nvidia-gpu-notebooks"))
	})

	t.Run("should migrate valid container size annotation to HWP annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		notebook := createTestNotebook(namespace, "notebook-with-size")
		notebook.SetAnnotations(map[string]string{
			"notebooks.opendatahub.io/last-size-selection": "X Large",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, notebook))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation was added
		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-with-size", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-x-large-notebooks"))
	})

	t.Run("should not migrate invalid container size annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		notebook := createTestNotebook(namespace, "notebook-with-invalid-size")
		notebook.SetAnnotations(map[string]string{
			"notebooks.opendatahub.io/last-size-selection": "InvalidSize",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, notebook))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation was NOT added (original annotation remains)
		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-with-invalid-size", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
		g.Expect(updatedNotebook.GetAnnotations()).To(HaveKeyWithValue("notebooks.opendatahub.io/last-size-selection", "InvalidSize"))
	})

	t.Run("should handle multiple notebooks with mixed scenarios", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		notebook1 := createTestNotebook(namespace, "notebook-ap")
		notebook1.SetAnnotations(map[string]string{
			"opendatahub.io/accelerator-name": "gpu-1",
		})

		notebook2 := createTestNotebook(namespace, "notebook-size")
		notebook2.SetAnnotations(map[string]string{
			"notebooks.opendatahub.io/last-size-selection": "Medium",
		})

		notebook3 := createTestNotebook(namespace, "notebook-existing-hwp")
		notebook3.SetAnnotations(map[string]string{
			"opendatahub.io/hardware-profile-name": "already-set",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, notebook1, notebook2, notebook3))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify first notebook
		nb1 := &unstructured.Unstructured{}
		nb1.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-ap", Namespace: namespace}, nb1)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(nb1.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "gpu-1-notebooks"))

		// Verify second notebook
		nb2 := &unstructured.Unstructured{}
		nb2.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-size", Namespace: namespace}, nb2)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(nb2.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-medium-notebooks"))

		// Verify third notebook
		nb3 := &unstructured.Unstructured{}
		nb3.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-existing-hwp", Namespace: namespace}, nb3)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(nb3.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "already-set"))
	})

	t.Run("should handle no notebooks gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}

func TestAttachHardwareProfileToInferenceServices(t *testing.T) {
	ctx := context.Background()
	namespace := "test-namespace"

	t.Run("should migrate AP annotation from ServingRuntime to InferenceService", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		servingRuntime := createTestServingRuntime(namespace, "test-runtime")
		servingRuntime.SetAnnotations(map[string]string{
			"opendatahub.io/accelerator-name": "nvidia Gpu",
		})

		isvc := createTestInferenceService(namespace, "isvc-with-runtime", "test-runtime")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, servingRuntime, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation was added to InferenceService
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-with-runtime", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "nvidia-gpu-serving"))
	})

	t.Run("should match container size for InferenceService without AP", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		isvc := createTestInferenceServiceWithResources(namespace, "isvc-with-resources",
			"1", "4Gi", "2", "8Gi")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation matches container size
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-with-resources", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-small-serving"))
	})

	t.Run("should use custom-serving for non-matching resources", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		isvc := createTestInferenceServiceWithResources(namespace, "isvc-custom",
			"3", "10Gi", "5", "20Gi")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify custom-serving HWP annotation
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-custom", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "custom-serving"))
	})

	t.Run("should use custom-serving for InferenceService without resources", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		isvc := createTestInferenceService(namespace, "isvc-no-resources", "")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify custom-serving HWP annotation
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-no-resources", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "custom-serving"))
	})

	t.Run("should skip InferenceService that already has HWP annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		isvc := createTestInferenceService(namespace, "isvc-with-hwp", "")
		isvc.SetAnnotations(map[string]string{
			"opendatahub.io/hardware-profile-name": "existing-hwp",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation remains unchanged
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-with-hwp", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "existing-hwp"))
	})

	t.Run("should handle no InferenceServices gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}

// Helper function to create test Notebook.
func createTestNotebook(namespace, name string) *unstructured.Unstructured {
	notebook := &unstructured.Unstructured{}
	notebook.SetGroupVersionKind(gvk.Notebook)
	notebook.SetName(name)
	notebook.SetNamespace(namespace)
	return notebook
}

// Helper function to create test ServingRuntime.
func createTestServingRuntime(namespace, name string) *unstructured.Unstructured {
	servingRuntime := &unstructured.Unstructured{}
	servingRuntime.SetGroupVersionKind(gvk.ServingRuntime)
	servingRuntime.SetName(name)
	servingRuntime.SetNamespace(namespace)
	return servingRuntime
}

// Helper function to create test InferenceService.
func createTestInferenceService(namespace, name, runtimeName string) *unstructured.Unstructured {
	isvc := &unstructured.Unstructured{}
	isvc.SetGroupVersionKind(gvk.InferenceServices)
	isvc.SetName(name)
	isvc.SetNamespace(namespace)

	if runtimeName != "" {
		spec := map[string]interface{}{
			"predictor": map[string]interface{}{
				"model": map[string]interface{}{
					"runtime": runtimeName,
				},
			},
		}
		isvc.Object["spec"] = spec
	}

	return isvc
}

// Helper function to create test InferenceService with resources.
func createTestInferenceServiceWithResources(namespace, name, reqCpu, reqMem, limCpu, limMem string) *unstructured.Unstructured {
	isvc := &unstructured.Unstructured{}
	isvc.SetGroupVersionKind(gvk.InferenceServices)
	isvc.SetName(name)
	isvc.SetNamespace(namespace)

	spec := map[string]interface{}{
		"predictor": map[string]interface{}{
			"model": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    reqCpu,
						"memory": reqMem,
					},
					"limits": map[string]interface{}{
						"cpu":    limCpu,
						"memory": limMem,
					},
				},
			},
		},
	}
	isvc.Object["spec"] = spec

	return isvc
}
