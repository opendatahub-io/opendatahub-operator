package upgrade_test

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	hardwareProfileKind  = "HardwareProfile"
	notebooksProfileType = "notebooks"
)

func TestGetOdhDashboardConfigWithMissingCRD(t *testing.T) {
	ctx := t.Context()

	t.Run("should handle NotFound error when OdhDashboardConfig resource doesn't exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create a fake client with no OdhDashboardConfig
		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		// When not found in cluster, it attempts to load from manifests
		// If manifests don't exist, it will return a file not found error
		_, found, err := upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")

		// The function returns error when manifest file is not found
		// This is expected behavior when both cluster and manifest sources fail
		if err != nil {
			g.Expect(err.Error()).To(ContainSubstring("failed to load OdhDashboardConfig from manifests"))
		} else {
			// If somehow manifests are available in test env, found should be false
			g.Expect(found).To(BeFalse())
		}
	})

	t.Run("should handle NoMatchError when OdhDashboardConfig CRD is not installed", func(t *testing.T) {
		g := NewWithT(t)

		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate the CRD not being installed
				if obj.GetObjectKind().GroupVersionKind().Kind == "OdhDashboardConfig" {
					return &meta.NoKindMatchError{
						GroupKind: schema.GroupKind{
							Group: "opendatahub.io",
							Kind:  "OdhDashboardConfig",
						},
						SearchedVersions: []string{"v1alpha1"},
					}
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// When NoMatchError occurs (CRD missing), it should not fail immediately
		// It attempts to load from manifests as a fallback
		_, found, err := upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")

		// If manifests don't exist, it will return error
		// This is expected behavior when both cluster and manifest sources fail
		if err != nil {
			g.Expect(err.Error()).To(ContainSubstring("failed to load OdhDashboardConfig from manifests"))
		} else {
			// If somehow manifests are available in test env, found should be false
			g.Expect(found).To(BeFalse())
		}
	})

	t.Run("should return error for other types of errors", func(t *testing.T) {
		g := NewWithT(t)

		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate a different error (e.g., network error, permission error)
				if obj.GetObjectKind().GroupVersionKind().Kind == "OdhDashboardConfig" {
					return k8serr.NewInternalError(errors.New("internal server error"))
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// This should return the error since it's not NotFound or NoMatch
		_, _, err = upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to get OdhDashboardConfig from cluster"))
		g.Expect(err.Error()).To(ContainSubstring("internal server error"))
	})

	t.Run("should successfully get OdhDashboardConfig when it exists in cluster", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-app-ns")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		// This should successfully retrieve the OdhDashboardConfig
		config, found, err := upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
		g.Expect(config).ToNot(BeNil())
		g.Expect(config.GetName()).To(Equal("odh-dashboard-config"))
		g.Expect(config.GetNamespace()).To(Equal("test-app-ns"))
	})
}

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
		err = upgrade.CleanupExistingResource(ctx, cli)
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
		err = upgrade.CleanupExistingResource(ctx, cli)
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
		err = upgrade.CleanupExistingResource(ctx, cli)
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

	t.Run("should skip Serverless InferenceService with deploymentMode annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		isvc := createTestInferenceService(namespace, "isvc-serverless-annotation", "")
		isvc.SetAnnotations(map[string]string{
			"serving.kserve.io/deploymentMode": "Serverless",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HWP annotation added (Serverless ISVC should be skipped)
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-serverless-annotation", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
	})

	t.Run("should skip Serverless InferenceService with deploymentMode in status", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(namespace)
		isvc := createTestInferenceService(namespace, "isvc-serverless-status", "")

		// Set deploymentMode in status
		status := map[string]interface{}{
			"deploymentMode": "Serverless",
		}
		isvc.Object["status"] = status

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HWP annotation added (Serverless ISVC should be skipped)
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-serverless-status", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
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

func TestCleanupExistingResourceWithCRDChecks(t *testing.T) {
	ctx := t.Context()

	t.Run("should complete without error when CRD checks succeed", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create OdhDashboardConfig for migration
		odhConfig := createTestOdhDashboardConfig("test-app-ns")

		// Create AcceleratorProfile to migrate
		ap := createTestAcceleratorProfile("test-app-ns")

		// Create fake client - types are registered in the scheme, so HasCRD will succeed
		cli, err := fakeclient.New(fakeclient.WithObjects(dsci, odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource - should complete without error
		// The actual migration behavior is tested in TestMigrateAcceleratorProfilesToHardwareProfiles
		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should skip HardwareProfile migration when infrastructure HWP CRD does not exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create AcceleratorProfile but simulate missing infrastructure HWP CRD
		ap := createTestAcceleratorProfile("test-app-ns")

		// Intercept HasCRD to simulate missing infrastructure HWP CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate CRD not found for infrastructure HardwareProfile
				if key.Name == "hardwareprofiles.infrastructure.opendatahub.io" {
					return k8serr.NewNotFound(schema.GroupResource{
						Group:    "apiextensions.k8s.io",
						Resource: "customresourcedefinitions",
					}, key.Name)
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci, ap),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-app-ns"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hwpList.Items).To(BeEmpty(), "HardwareProfiles should not be created when infrastructure CRD missing")
	})

	t.Run("should skip HardwareProfile migration when AcceleratorProfile CRD does not exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Intercept HasCRD to simulate missing AcceleratorProfile CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate CRD not found for dashboard AcceleratorProfile
				if key.Name == "acceleratorprofiles.dashboard.opendatahub.io" {
					return k8serr.NewNotFound(schema.GroupResource{
						Group:    "apiextensions.k8s.io",
						Resource: "customresourcedefinitions",
					}, key.Name)
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-app-ns"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hwpList.Items).To(BeEmpty(), "HardwareProfiles should not be created when AcceleratorProfile CRD missing")
	})

	t.Run("should run GatewayConfig migration when GatewayConfig CRD exists", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create GatewayConfig without ingressMode set
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{}
		err = unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		cli, err := fakeclient.New(fakeclient.WithObjects(dsci, gatewayConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// GatewayConfig migration should have been attempted (no error means it ran)
		// We can't verify the migration result without creating a Gateway service,
		// but we verify no error occurred
	})

	t.Run("should skip GatewayConfig migration when GatewayConfig CRD does not exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Intercept HasCRD to simulate missing GatewayConfig CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate CRD not found for GatewayConfig
				if key.Name == "gatewayconfigs.services.platform.opendatahub.io" {
					return k8serr.NewNotFound(schema.GroupResource{
						Group:    "apiextensions.k8s.io",
						Resource: "customresourcedefinitions",
					}, key.Name)
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// No error should occur, migration should be skipped silently
	})

	t.Run("should handle CRD check errors gracefully", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Intercept to simulate error checking CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Name == "hardwareprofiles.infrastructure.opendatahub.io" {
					return errors.New("simulated API server error")
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)

		// Should return error containing CRD check failure
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to check HardwareProfile CRD"))
	})

	t.Run("should handle no DSCI gracefully", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}

// ClusterState represents a snapshot of cluster resources for idempotence verification.
// This struct deliberately tracks ONLY the resources modified by MigrateToInfraHardwareProfiles:
//   - HardwareProfiles: Created during migration from AcceleratorProfiles and container sizes
//   - NotebookAnnotations: Updated with hardware-profile-name and hardware-profile-namespace annotations
//   - ISVCAnnotations: Updated with hardware-profile-name and hardware-profile-namespace annotations
//
// Other resources managed by CleanupExistingResource (RoleBindings, Deployments, etc.) are intentionally
// excluded as they are not relevant to the HardwareProfile migration idempotence tests.
type ClusterState struct {
	HardwareProfiles    []infrav1.HardwareProfile
	NotebookAnnotations map[string]map[string]string // Map: notebook name -> annotations
	ISVCAnnotations     map[string]map[string]string // Map: ISVC name -> annotations
}

// captureClusterState captures the current state of resources modified by MigrateToInfraHardwareProfiles.
// This function lists all HardwareProfiles in the specified namespace and captures annotations from
// all Notebooks and InferenceServices (across all namespaces) for comparison.
//
// Returns:
//   - *ClusterState: Snapshot of current cluster state
//   - error: Any errors encountered during state capture (except NoMatchError which is ignored)
func captureClusterState(ctx context.Context, cli client.Client, namespace string) (*ClusterState, error) {
	state := &ClusterState{
		NotebookAnnotations: make(map[string]map[string]string),
		ISVCAnnotations:     make(map[string]map[string]string),
	}

	// Capture HardwareProfiles
	var hwpList infrav1.HardwareProfileList
	if err := cli.List(ctx, &hwpList, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to list HardwareProfiles: %w", err)
	}
	state.HardwareProfiles = hwpList.Items

	// Capture Notebook annotations
	notebookList := &unstructured.UnstructuredList{}
	notebookList.SetGroupVersionKind(gvk.Notebook)
	if err := cli.List(ctx, notebookList); err != nil && !meta.IsNoMatchError(err) {
		return nil, fmt.Errorf("failed to list notebooks: %w", err)
	}
	for i := range notebookList.Items {
		nb := &notebookList.Items[i]
		annotations := nb.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		state.NotebookAnnotations[nb.GetName()] = annotations
	}

	// Capture InferenceService annotations
	isvcList := &unstructured.UnstructuredList{}
	isvcList.SetGroupVersionKind(gvk.InferenceServices)
	if err := cli.List(ctx, isvcList); err != nil && !meta.IsNoMatchError(err) {
		return nil, fmt.Errorf("failed to list InferenceServices: %w", err)
	}
	for i := range isvcList.Items {
		isvc := &isvcList.Items[i]
		annotations := isvc.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		state.ISVCAnnotations[isvc.GetName()] = annotations
	}

	return state, nil
}

// compareClusterStates performs a deep comparison of two cluster states for idempotence verification.
// This function checks that running the migration multiple times produces identical results.
//
// Comparison logic:
//   - HardwareProfiles: Compares count, names, and specs using reflect.DeepEqual
//   - Notebook/ISVC annotations: Compares all annotations except timestamps (opendatahub.io/modified-date)
//
// Returns:
//   - bool: true if states are identical (idempotent), false if differences found
//   - []string: List of specific differences found (empty if identical)
func compareClusterStates(before, after *ClusterState) (bool, []string) {
	var differences []string

	// Compare HardwareProfiles count and names
	if len(before.HardwareProfiles) != len(after.HardwareProfiles) {
		differences = append(differences, fmt.Sprintf("HardwareProfile count changed: %d -> %d",
			len(before.HardwareProfiles), len(after.HardwareProfiles)))
	}

	// Create maps for quick lookup and deep comparison
	beforeHWPMap := make(map[string]*infrav1.HardwareProfile)
	for i := range before.HardwareProfiles {
		hwp := &before.HardwareProfiles[i]
		beforeHWPMap[hwp.Name] = hwp
	}
	afterHWPMap := make(map[string]*infrav1.HardwareProfile)
	for i := range after.HardwareProfiles {
		hwp := &after.HardwareProfiles[i]
		afterHWPMap[hwp.Name] = hwp
	}

	// Check for disappeared HardwareProfiles
	for name := range beforeHWPMap {
		if _, exists := afterHWPMap[name]; !exists {
			differences = append(differences, fmt.Sprintf("HardwareProfile %s disappeared", name))
		}
	}

	// Check for appeared HardwareProfiles
	for name := range afterHWPMap {
		if _, exists := beforeHWPMap[name]; !exists {
			differences = append(differences, fmt.Sprintf("HardwareProfile %s appeared", name))
		}
	}

	// Deep compare specs for HardwareProfiles that exist in both states
	for name, beforeHWP := range beforeHWPMap {
		if afterHWP, exists := afterHWPMap[name]; exists {
			if !reflect.DeepEqual(beforeHWP.Spec, afterHWP.Spec) {
				differences = append(differences, fmt.Sprintf("HardwareProfile %s spec changed", name))
			}
		}
	}

	// Compare Notebook annotations (excluding timestamp annotations)
	notebookDiffs := compareAnnotationMaps("Notebook", before.NotebookAnnotations, after.NotebookAnnotations)
	differences = append(differences, notebookDiffs...)

	// Compare InferenceService annotations (excluding timestamp annotations)
	isvcDiffs := compareAnnotationMaps("InferenceService", before.ISVCAnnotations, after.ISVCAnnotations)
	differences = append(differences, isvcDiffs...)

	return len(differences) == 0, differences
}

// compareAnnotationMaps compares annotations on resources (Notebooks or InferenceServices), ignoring timestamps.
// This function provides detailed difference reporting for idempotence verification.
//
// Parameters:
//   - resourceType: Type of resource being compared (e.g., "Notebook", "InferenceService")
//   - before: Map of resource name to annotations from first state capture
//   - after: Map of resource name to annotations from second state capture
//
// Returns:
//   - []string: List of specific differences with details (added/removed/changed annotations)
//
// The function ignores the "opendatahub.io/modified-date" annotation as it changes on every update.
func compareAnnotationMaps(resourceType string, before, after map[string]map[string]string) []string {
	var differences []string

	// Check for resources that disappeared
	for name := range before {
		if _, exists := after[name]; !exists {
			differences = append(differences, fmt.Sprintf("%s %s disappeared", resourceType, name))
		}
	}

	// Check for resources that appeared
	for name := range after {
		if _, exists := before[name]; !exists {
			differences = append(differences, fmt.Sprintf("%s %s appeared", resourceType, name))
		}
	}

	// Compare annotations for resources that exist in both states
	for name, beforeAnnotations := range before {
		afterAnnotations, exists := after[name]
		if !exists {
			continue // Already reported as disappeared
		}

		// Compare annotations excluding timestamps
		for key, beforeValue := range beforeAnnotations {
			// Skip timestamp annotations
			if key == "opendatahub.io/modified-date" {
				continue
			}

			afterValue, exists := afterAnnotations[key]
			if !exists {
				differences = append(differences, fmt.Sprintf("%s %s annotation %s was removed", resourceType, name, key))
			} else if beforeValue != afterValue {
				differences = append(differences, fmt.Sprintf("%s %s annotation %s changed: %q -> %q", resourceType, name, key, beforeValue, afterValue))
			}
		}

		// Check for new annotations (excluding timestamps)
		for key, afterValue := range afterAnnotations {
			if key == "opendatahub.io/modified-date" {
				continue
			}
			if _, exists := beforeAnnotations[key]; !exists {
				differences = append(differences, fmt.Sprintf("%s %s annotation %s was added: %q", resourceType, name, key, afterValue))
			}
		}
	}

	return differences
}

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
func TestMigrateToInfraHardwareProfilesIdempotence(t *testing.T) {
	ctx := t.Context()

	t.Run("full migration idempotence", func(t *testing.T) {
		g := NewWithT(t)
		namespace := "test-idempotence-ns"

		// Create OdhDashboardConfig
		odhConfig := createTestOdhDashboardConfig(namespace)

		// Create AcceleratorProfiles
		ap1 := createTestAcceleratorProfile(namespace)
		ap1.SetName("gpu-profile")

		ap2 := createTestAcceleratorProfile(namespace)
		ap2.SetName("tpu-profile")
		spec2, _, _ := unstructured.NestedMap(ap2.Object, "spec")
		spec2["identifier"] = "google.com/tpu"
		spec2["displayName"] = "TPU"
		err := unstructured.SetNestedMap(ap2.Object, spec2, "spec")
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
		g.Expect(len(stateAfterFirstRun.HardwareProfiles)).To(BeNumerically(">", 0), "HardwareProfiles should be created")

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

		// Second run of migration (test idempotence)
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred(), "Second run should complete without errors")

		// Capture state after second run
		stateAfterSecondRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Compare states - should be identical
		identical, differences := compareClusterStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical after second run. Differences: %v", differences)

		// Third run to be extra sure
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred(), "Third run should complete without errors")

		stateAfterThirdRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareClusterStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical after third run. Differences: %v", differences)
	})

	t.Run("partial completion - some HWPs already exist", func(t *testing.T) {
		g := NewWithT(t)
		namespace := "test-partial-ns"

		odhConfig := createTestOdhDashboardConfig(namespace)
		ap := createTestAcceleratorProfile(namespace)

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

		// First run - should handle existing HWP gracefully
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Second run - test idempotence
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareClusterStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)
	})

	t.Run("preserves user modifications to existing HWPs", func(t *testing.T) {
		g := NewWithT(t)
		namespace := "test-preserve-modifications-ns"

		odhConfig := createTestOdhDashboardConfig(namespace)
		ap := createTestAcceleratorProfile(namespace)

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
	})

	t.Run("partial completion - some annotations already set", func(t *testing.T) {
		g := NewWithT(t)
		namespace := "test-partial-annotations-ns"

		odhConfig := createTestOdhDashboardConfig(namespace)
		ap := createTestAcceleratorProfile(namespace)

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

		// Second run - test idempotence
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareClusterStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)
	})

	t.Run("edge case - all already migrated", func(t *testing.T) {
		g := NewWithT(t)
		namespace := "test-all-migrated-ns"

		odhConfig := createTestOdhDashboardConfig(namespace)

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

		// First run
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Second run
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareClusterStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)
	})

	t.Run("concurrent changes - new resources added between runs", func(t *testing.T) {
		g := NewWithT(t)
		namespace := "test-concurrent-ns"

		odhConfig := createTestOdhDashboardConfig(namespace)
		ap1 := createTestAcceleratorProfile(namespace)
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
		ap2 := createTestAcceleratorProfile(namespace)
		ap2.SetName("new-ap")
		spec, _, _ := unstructured.NestedMap(ap2.Object, "spec")
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

		// Third run - should be idempotent with the new state
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareClusterStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should be identical after handling new resources. Differences: %v", differences)
	})

	t.Run("mixed state - some notebooks migrated, some not", func(t *testing.T) {
		g := NewWithT(t)
		namespace := "test-mixed-state-ns"

		odhConfig := createTestOdhDashboardConfig(namespace)
		ap := createTestAcceleratorProfile(namespace)

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

		// Second run - test idempotence
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareClusterStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)

		// Third run
		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureClusterState(ctx, cli, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareClusterStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("empty namespace - migration skipped idempotently", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig("test-namespace")
		ap := createTestAcceleratorProfile("test-namespace")

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
	})
}

// GatewayConfigState represents a snapshot of GatewayConfig state for idempotence verification.
type GatewayConfigState struct {
	Exists      bool
	IngressMode string
}

// captureGatewayConfigState captures the current state of the GatewayConfig resource.
// Returns the ingressMode value and whether the GatewayConfig exists.
func captureGatewayConfigState(ctx context.Context, cli client.Client) (*GatewayConfigState, error) {
	state := &GatewayConfigState{}

	gatewayConfig := &unstructured.Unstructured{}
	gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)

	err := cli.Get(ctx, client.ObjectKey{Name: "default-gateway"}, gatewayConfig)
	if k8serr.IsNotFound(err) {
		state.Exists = false
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get GatewayConfig: %w", err)
	}

	state.Exists = true
	// Error ignored: field may not exist, which is a valid state (ingressMode not yet set)
	ingressMode, _, _ := unstructured.NestedString(gatewayConfig.Object, "spec", "ingressMode")
	state.IngressMode = ingressMode

	return state, nil
}

// compareGatewayConfigStates compares two GatewayConfig states for idempotence verification.
// Returns true if states are identical, false otherwise with a list of differences.
func compareGatewayConfigStates(before, after *GatewayConfigState) (bool, []string) {
	var differences []string

	if before.Exists != after.Exists {
		differences = append(differences, fmt.Sprintf("GatewayConfig existence changed: %v -> %v", before.Exists, after.Exists))
	}

	if before.IngressMode != after.IngressMode {
		differences = append(differences, fmt.Sprintf("ingressMode changed: %q -> %q", before.IngressMode, after.IngressMode))
	}

	return len(differences) == 0, differences
}

// TestMigrateGatewayConfigIngressModeIdempotence verifies the idempotence of GatewayConfig ingressMode migration.
//
// Idempotence Definition:
// The migration function can be run multiple times without:
//   - Producing errors (e.g., attempting to patch when already set)
//   - Changing the final cluster state unnecessarily
//   - Modifying resources when migration conditions are not met
//
// Test Strategy:
// Each test scenario runs the migration 2-3 times and compares GatewayConfig state snapshots to ensure
// they remain identical. The test captures the GatewayConfig's ingressMode before and after each run.
//
// Test Scenarios:
//  1. No GatewayConfig exists - idempotent no-op
//  2. GatewayConfig without ingressMode + LoadBalancer service - sets once, then no-op
//  3. GatewayConfig already has ingressMode - always no-op
//  4. GatewayConfig without ingressMode, no service - always no-op
//  5. GatewayConfig without ingressMode + ClusterIP service - always no-op
//  6. Service added between runs - handles new conditions, then idempotent
//  7. User modifies ingressMode between runs - preserves user change
func TestMigrateGatewayConfigIngressModeIdempotence(t *testing.T) {
	ctx := t.Context()

	t.Run("no GatewayConfig exists - idempotent no-op", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - no GatewayConfig
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.Exists).To(BeFalse(), "GatewayConfig should not exist")

		// Second run - should still be no-op
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical (both non-existent). Differences: %v", differences)

		// Third run
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("GatewayConfig without ingressMode + LoadBalancer service - sets once then no-op", func(t *testing.T) {
		g := NewWithT(t)

		// Create GatewayConfig without ingressMode
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{}
		err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create LoadBalancer service
		gatewayService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-science-gateway-data-science-gateway-class",
				Namespace: "openshift-ingress",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - should set ingressMode
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
		g.Expect(stateAfterFirstRun.IngressMode).To(Equal("LoadBalancer"), "ingressMode should be set to LoadBalancer")

		// Second run - should be no-op (ingressMode already set)
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical after second run. Differences: %v", differences)

		// Third run - should still be no-op
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("GatewayConfig already has ingressMode - always no-op", func(t *testing.T) {
		g := NewWithT(t)

		// Create GatewayConfig with ingressMode already set
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{
			"ingressMode": "LoadBalancer",
		}
		err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create LoadBalancer service
		gatewayService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-science-gateway-data-science-gateway-class",
				Namespace: "openshift-ingress",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - should be no-op
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
		g.Expect(stateAfterFirstRun.IngressMode).To(Equal("LoadBalancer"), "ingressMode should remain LoadBalancer")

		// Second run - should still be no-op
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)

		// Third run
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("GatewayConfig without ingressMode, no Gateway service - always no-op", func(t *testing.T) {
		g := NewWithT(t)

		// Create GatewayConfig without ingressMode
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{}
		err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		// No Gateway service created

		cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - should be no-op (no service)
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
		g.Expect(stateAfterFirstRun.IngressMode).To(BeEmpty(), "ingressMode should remain empty")

		// Second run - should still be no-op
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)

		// Third run
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("GatewayConfig without ingressMode + ClusterIP service - always no-op", func(t *testing.T) {
		g := NewWithT(t)

		// Create GatewayConfig without ingressMode
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{}
		err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create ClusterIP service (not LoadBalancer)
		gatewayService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-science-gateway-data-science-gateway-class",
				Namespace: "openshift-ingress",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - should be no-op (not LoadBalancer)
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
		g.Expect(stateAfterFirstRun.IngressMode).To(BeEmpty(), "ingressMode should remain empty for ClusterIP service")

		// Second run - should still be no-op
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)

		// Third run
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("LoadBalancer service added between runs - handles new conditions then idempotent", func(t *testing.T) {
		g := NewWithT(t)

		// Create GatewayConfig without ingressMode
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{}
		err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		// No service initially
		cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - no service, should be no-op
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
		g.Expect(stateAfterFirstRun.IngressMode).To(BeEmpty(), "ingressMode should be empty without service")

		// Add LoadBalancer service
		gatewayService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-science-gateway-data-science-gateway-class",
				Namespace: "openshift-ingress",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
		}
		err = cli.Create(ctx, gatewayService)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Second run - service now exists, should set ingressMode
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterSecondRun.Exists).To(BeTrue())
		g.Expect(stateAfterSecondRun.IngressMode).To(Equal("LoadBalancer"), "ingressMode should be set with service present")

		// Third run - should be no-op (ingressMode already set)
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should be identical after service addition. Differences: %v", differences)

		// Fourth run - verify continued idempotence
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFourthRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterThirdRun, stateAfterFourthRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("user modifies ingressMode between runs - preserves user change", func(t *testing.T) {
		g := NewWithT(t)

		// Create GatewayConfig without ingressMode
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{}
		err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create LoadBalancer service
		gatewayService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-science-gateway-data-science-gateway-class",
				Namespace: "openshift-ingress",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - should set ingressMode to LoadBalancer
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.IngressMode).To(Equal("LoadBalancer"))

		// Simulate user changing ingressMode to a custom value
		updatedGatewayConfig := &unstructured.Unstructured{}
		updatedGatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		err = cli.Get(ctx, client.ObjectKey{Name: "default-gateway"}, updatedGatewayConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = unstructured.SetNestedField(updatedGatewayConfig.Object, "Route", "spec", "ingressMode")
		g.Expect(err).ShouldNot(HaveOccurred())
		err = cli.Update(ctx, updatedGatewayConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify user's change
		stateAfterUserChange, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterUserChange.IngressMode).To(Equal("Route"), "User's custom ingressMode should be set")

		// Second run - should preserve user's change (not overwrite)
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterUserChange, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "User's custom ingressMode should be preserved. Differences: %v", differences)
		g.Expect(stateAfterSecondRun.IngressMode).To(Equal("Route"), "User's custom ingressMode should not be overwritten")

		// Third run - verify continued preservation
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})

	t.Run("NodePort service type - always no-op", func(t *testing.T) {
		g := NewWithT(t)

		// Create GatewayConfig without ingressMode
		gatewayConfig := &unstructured.Unstructured{}
		gatewayConfig.SetGroupVersionKind(gvk.GatewayConfig)
		gatewayConfig.SetName("default-gateway")
		spec := map[string]interface{}{}
		err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create NodePort service (not LoadBalancer)
		gatewayService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-science-gateway-data-science-gateway-class",
				Namespace: "openshift-ingress",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
		g.Expect(err).ShouldNot(HaveOccurred())

		// First run - should be no-op (not LoadBalancer)
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
		g.Expect(stateAfterFirstRun.IngressMode).To(BeEmpty(), "ingressMode should remain empty for NodePort service")

		// Second run
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences := compareGatewayConfigStates(stateAfterFirstRun, stateAfterSecondRun)
		g.Expect(identical).To(BeTrue(), "States should be identical. Differences: %v", differences)

		// Third run
		err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		stateAfterThirdRun, err := captureGatewayConfigState(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		identical, differences = compareGatewayConfigStates(stateAfterSecondRun, stateAfterThirdRun)
		g.Expect(identical).To(BeTrue(), "States should remain identical. Differences: %v", differences)
	})
}
