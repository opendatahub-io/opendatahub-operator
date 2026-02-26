package upgrade_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestMigrateAcceleratorProfilesToHardwareProfiles(t *testing.T) {
	ctx := t.Context()

	t.Run("should migrate AcceleratorProfiles to HardwareProfiles successfully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")
		ap := createTestAcceleratorProfile(t, "test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Should have 2 HWPs (notebooks and serving) for the AP
		notebooksHWP, found := findHardwareProfileByName(&hwpList, "test-ap-notebooks")
		g.Expect(found).To(BeTrue(), "notebooks HWP should exist")
		servingHWP, found := findHardwareProfileByName(&hwpList, "test-ap-serving")
		g.Expect(found).To(BeTrue(), "serving HWP should exist")

		// Validate notebooks HWP
		validateAcceleratorProfileHardwareProfile(g, notebooksHWP, ap, odhConfig, "notebooks")

		// Validate serving HWP
		validateAcceleratorProfileHardwareProfile(g, servingHWP, ap, odhConfig, "serving")
	})

	t.Run("should handle empty AcceleratorProfile list", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should handle AcceleratorProfile with tolerations", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithTolerations(t, "test-namespace")
		ap := createTestAcceleratorProfileWithTolerations(t, "test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateAcceleratorProfilesToHardwareProfiles(ctx, cli, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfile has tolerations
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		notebooksHWP, found := findHardwareProfileByName(&hwpList, "test-ap-notebooks")
		g.Expect(found).To(BeTrue(), "notebooks HWP should exist")
		g.Expect(notebooksHWP.Spec.SchedulingSpec).ToNot(BeNil())
		g.Expect(notebooksHWP.Spec.SchedulingSpec.Node.Tolerations).ToNot(BeEmpty())
	})

	t.Run("should handle malformed AcceleratorProfile gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")
		ap := createMalformedAcceleratorProfile(t, "test-namespace")

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

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateContainerSizesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfiles were created for container sizes
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Get container size data from OdhDashboardConfig
		notebookSizes, found, err := unstructured.NestedSlice(odhConfig.Object, "spec", "notebookSizes")
		g.Expect(err).ShouldNot(HaveOccurred(), "failed to get notebookSizes from OdhDashboardConfig")
		g.Expect(found).To(BeTrue(), "notebookSizes not found in OdhDashboardConfig")

		modelServerSizes, found, err := unstructured.NestedSlice(odhConfig.Object, "spec", "modelServerSizes")
		g.Expect(err).ShouldNot(HaveOccurred(), "failed to get modelServerSizes from OdhDashboardConfig")
		g.Expect(found).To(BeTrue(), "modelServerSizes not found in OdhDashboardConfig")

		// Validate notebooks size HWP
		if len(notebookSizes) > 0 {
			for _, nb := range notebookSizes {
				containerSize, ok := nb.(map[string]interface{})
				if ok {
					name, _ := containerSize["name"].(string)
					hwpName := strings.ReplaceAll(strings.ToLower(fmt.Sprintf("containerSize-%s-notebooks", name)), " ", "-")
					notebooksSizeHWP, found := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(found).To(BeTrue(), "notebooks size HWP %s should exist", hwpName)
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
					modelServerSizeHWP, found := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(found).To(BeTrue(), "model server size HWP %s should exist", hwpName)
					validateContainerSizeHardwareProfile(g, modelServerSizeHWP, containerSize, odhConfig, "serving")
				}
			}
		}
	})

	t.Run("should handle missing container sizes gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithoutSizes(t, "test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateContainerSizesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should handle malformed container sizes gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithMalformedSizes(t, "test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateContainerSizesToHardwareProfiles(ctx, cli, "test-namespace", odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred()) // Should skip malformed sizes
	})
}

func TestHardwareProfileMigrationErrorAggregation(t *testing.T) {
	ctx := t.Context()

	t.Run("should aggregate multiple errors from different migration steps", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")
		ap := createTestAcceleratorProfile(t, "test-namespace")

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

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")
		ap := createTestAcceleratorProfile(t, "test-namespace")

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

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")
		ap1 := createTestAcceleratorProfile(t, "test-namespace")
		ap1.SetName("gpu-ap")

		ap2 := createTestAcceleratorProfile(t, "test-namespace")
		ap2.SetName("cpu-ap")
		// Modify the second AP to have different identifier
		spec, found, err := unstructured.NestedMap(ap2.Object, "spec")
		g.Expect(err).ShouldNot(HaveOccurred(), "failed to get spec from AcceleratorProfile")
		g.Expect(found).To(BeTrue(), "spec not found in AcceleratorProfile")

		spec["identifier"] = "cpu"
		err = unstructured.SetNestedMap(ap2.Object, spec, "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap1, ap2))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "test-namespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify multiple HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Find notebooks and serving HWPs
		gpuNotebooksHWP, found := findHardwareProfileByName(&hwpList, "gpu-ap-notebooks")
		g.Expect(found).To(BeTrue(), "gpu notebooks HWP should exist")
		gpuServingHWP, found := findHardwareProfileByName(&hwpList, "gpu-ap-serving")
		g.Expect(found).To(BeTrue(), "gpu serving HWP should exist")
		cpuNotebooksHWP, found := findHardwareProfileByName(&hwpList, "cpu-ap-notebooks")
		g.Expect(found).To(BeTrue(), "cpu notebooks HWP should exist")
		cpuServingHWP, found := findHardwareProfileByName(&hwpList, "cpu-ap-serving")
		g.Expect(found).To(BeTrue(), "cpu serving HWP should exist")

		// Validate notebooks HWP
		validateAcceleratorProfileHardwareProfile(g, gpuNotebooksHWP, ap1, odhConfig, "notebooks")
		validateAcceleratorProfileHardwareProfile(g, cpuNotebooksHWP, ap2, odhConfig, "notebooks")
		// Validate serving HWP
		validateAcceleratorProfileHardwareProfile(g, gpuServingHWP, ap1, odhConfig, "serving")
		validateAcceleratorProfileHardwareProfile(g, cpuServingHWP, ap2, odhConfig, "serving")

		// Get container size data from OdhDashboardConfig
		notebookSizes, found, err := unstructured.NestedSlice(odhConfig.Object, "spec", "notebookSizes")
		g.Expect(err).ShouldNot(HaveOccurred(), "failed to get notebookSizes from OdhDashboardConfig")
		g.Expect(found).To(BeTrue(), "notebookSizes not found in OdhDashboardConfig")

		modelServerSizes, found, err := unstructured.NestedSlice(odhConfig.Object, "spec", "modelServerSizes")
		g.Expect(err).ShouldNot(HaveOccurred(), "failed to get modelServerSizes from OdhDashboardConfig")
		g.Expect(found).To(BeTrue(), "modelServerSizes not found in OdhDashboardConfig")

		if len(notebookSizes) > 0 {
			for _, nb := range notebookSizes {
				containerSize, ok := nb.(map[string]interface{})
				if ok {
					name, _ := containerSize["name"].(string)
					hwpName := strings.ReplaceAll(strings.ToLower(fmt.Sprintf("containerSize-%s-notebooks", name)), " ", "-")
					notebooksSizeHWP, found := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(found).To(BeTrue(), "notebooks size HWP %s should exist", hwpName)
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
					modelServerSizeHWP, found := findHardwareProfileByName(&hwpList, hwpName)

					g.Expect(found).To(BeTrue(), "model server size HWP %s should exist", hwpName)
					validateContainerSizeHardwareProfile(g, modelServerSizeHWP, containerSize, odhConfig, "serving")
				}
			}
		}
	})

	t.Run("should handle AcceleratorProfile with complex tolerations", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfigWithTolerations(t, "test-namespace")
		ap := createTestAcceleratorProfileWithTolerations(t, "test-namespace")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.MigrateToInfraHardwareProfiles(ctx, cli, "test-namespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HardwareProfile has both AP tolerations and notebooks-only tolerations
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-namespace"))
		g.Expect(err).ShouldNot(HaveOccurred())

		notebooksHWP, found := findHardwareProfileByName(&hwpList, "test-ap-notebooks")
		g.Expect(found).To(BeTrue(), "notebooks HWP should exist")
		g.Expect(notebooksHWP.Spec.SchedulingSpec).ToNot(BeNil())
		g.Expect(len(notebooksHWP.Spec.SchedulingSpec.Node.Tolerations)).To(BeNumerically(">=", 2)) // AP + notebooks-only tolerations

		validateAcceleratorProfileHardwareProfile(g, notebooksHWP, ap, odhConfig, "notebooks")
	})

	t.Run("should skip migration if application namespace is empty", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, "test-namespace")
		ap := createTestAcceleratorProfile(t, "test-namespace")

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
