package dashboard_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	dashboardctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

const (
	displayNameKey = "opendatahub.io/display-name"
	descriptionKey = "opendatahub.io/description"
	disabledKey    = "opendatahub.io/disabled"
	customKey      = "custom-annotation"
	customValue    = "custom-value"
	invalidSpec    = "invalid-spec"
)

// createDashboardHWP creates a dashboardctrl.DashboardHardwareProfile unstructured object with the specified parameters.
func createDashboardHWP(tb testing.TB, name string, enabled bool, nodeType string) *unstructured.Unstructured {
	tb.Helper()
	// Validate required parameters
	if name == "" {
		tb.Fatalf("name parameter cannot be empty")
	}
	if nodeType == "" {
		tb.Fatalf("nodeType parameter cannot be empty")
	}

	dashboardHWP := &unstructured.Unstructured{}
	dashboardHWP.SetGroupVersionKind(gvk.DashboardHardwareProfile)
	dashboardHWP.SetName(name)
	dashboardHWP.SetNamespace(TestNamespace)
	dashboardHWP.Object["spec"] = map[string]interface{}{
		"displayName": fmt.Sprintf("Display Name for %s", name),
		"enabled":     enabled,
		"description": fmt.Sprintf("Description for %s", name),
		"nodeSelector": map[string]interface{}{
			NodeTypeKey: nodeType,
		},
	}

	return dashboardHWP
}

func TestReconcileHardwareProfiles(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		setupRR     func(t *testing.T) *odhtypes.ReconciliationRequest
		expectError bool
		validate    func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "CRDNotExists",
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				cli, err := fakeclient.New()
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				return rr
			},
			expectError: false,
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				// No specific validation needed for CRD not exists case
			},
		},
		{
			name: "WithValidProfiles",
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				// Create multiple mock dashboard hardware profiles
				profile1 := createDashboardHWP(t, "profile1", true, "gpu")
				profile2 := createDashboardHWP(t, "profile2", true, "cpu")
				profile3 := createDashboardHWP(t, "profile3", false, "cpu") // Disabled profile

				cli, err := fakeclient.New(fakeclient.WithObjects(profile1, profile2, profile3))
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				return rr
			},
			expectError: false,
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				// Test ProcessHardwareProfile directly for each profile to bypass CRD check
				profile1 := createDashboardHWP(t, "profile1", true, "gpu")
				profile2 := createDashboardHWP(t, "profile2", true, "cpu")
				profile3 := createDashboardHWP(t, "profile3", false, "cpu")

				ctx := t.Context()
				logger := log.FromContext(ctx)

				g := gomega.NewWithT(t)
				err := dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *profile1)
				g.Expect(err).ShouldNot(gomega.HaveOccurred())

				err = dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *profile2)
				g.Expect(err).ShouldNot(gomega.HaveOccurred())

				// Disabled profile should not be processed
				err = dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *profile3)
				g.Expect(err).ShouldNot(gomega.HaveOccurred())
			},
		},
		{
			name: "WithConversionError",
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				// This test verifies that ProcessHardwareProfile returns an error
				// when the dashboard hardware profile has an invalid spec that cannot be converted.
				// We test ProcessHardwareProfile directly to avoid CRD check issues.

				// Create a mock dashboard hardware profile with invalid spec
				dashboardHWP := &unstructured.Unstructured{}
				dashboardHWP.SetGroupVersionKind(gvk.DashboardHardwareProfile)
				dashboardHWP.SetName("invalid-profile")
				dashboardHWP.SetNamespace(TestNamespace)
				dashboardHWP.Object["spec"] = invalidSpec // Invalid spec type

				cli, err := fakeclient.New(fakeclient.WithObjects(dashboardHWP))
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				return rr
			},
			expectError: false, // ReconcileHardwareProfiles won't fail, but ProcessHardwareProfile will
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				// Test ProcessHardwareProfile directly to verify conversion error
				dashboardHWP := &unstructured.Unstructured{}
				dashboardHWP.SetGroupVersionKind(gvk.DashboardHardwareProfile)
				dashboardHWP.SetName("invalid-profile")
				dashboardHWP.SetNamespace(TestNamespace)
				dashboardHWP.Object["spec"] = invalidSpec // Invalid spec type

				ctx := t.Context()
				logger := log.FromContext(ctx)

				g := gomega.NewWithT(t)
				err := dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *dashboardHWP)
				g.Expect(err).Should(gomega.HaveOccurred())
				g.Expect(err.Error()).Should(gomega.ContainSubstring("failed to convert dashboard hardware profile"))
			},
		},
		{
			name: "WithCreateError",
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				// This test verifies that the hardware profile processing returns an error
				// when the Create operation fails for HardwareProfile objects.
				// We test the ProcessHardwareProfile function directly to avoid CRD check issues.

				// Create a mock dashboard hardware profile
				dashboardHWP := &unstructured.Unstructured{}
				dashboardHWP.SetGroupVersionKind(gvk.DashboardHardwareProfile)
				dashboardHWP.SetName("test-profile")
				dashboardHWP.SetNamespace(TestNamespace)
				dashboardHWP.Object["spec"] = map[string]interface{}{
					"displayName": TestDisplayName,
					"enabled":     true,
					"description": TestDescription,
					"nodeSelector": map[string]interface{}{
						NodeTypeKey: "gpu",
					},
				}

				// Create a mock client that will fail on Create operations for HardwareProfile objects
				cli, err := fakeclient.New(
					fakeclient.WithObjects(dashboardHWP),
					fakeclient.WithInterceptorFuncs(interceptor.Funcs{
						Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
							// Fail on Create operations for HardwareProfile objects
							t.Logf("Create interceptor called for object: %s, type: %T", obj.GetName(), obj)

							// Check if it's an infrastructure HardwareProfile by type
							if _, ok := obj.(*infrav1.HardwareProfile); ok {
								t.Logf("Triggering create error for HardwareProfile")
								return errors.New("simulated create error")
							}
							return client.Create(ctx, obj, opts...)
						},
					}),
				)
				gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				return rr
			},
			expectError: false, // ReconcileHardwareProfiles won't fail, but ProcessHardwareProfile will
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				// Test ProcessHardwareProfile directly to verify create error
				dashboardHWP := &unstructured.Unstructured{}
				dashboardHWP.SetGroupVersionKind(gvk.DashboardHardwareProfile)
				dashboardHWP.SetName("test-profile")
				dashboardHWP.SetNamespace(TestNamespace)
				dashboardHWP.Object["spec"] = map[string]interface{}{
					"displayName": TestDisplayName,
					"enabled":     true,
					"description": TestDescription,
					"nodeSelector": map[string]interface{}{
						NodeTypeKey: "gpu",
					},
				}

				ctx := t.Context()
				logger := log.FromContext(ctx)

				g := gomega.NewWithT(t)
				err := dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *dashboardHWP)
				t.Logf("ProcessHardwareProfile returned error: %v", err)
				g.Expect(err).Should(gomega.HaveOccurred())
				g.Expect(err.Error()).Should(gomega.ContainSubstring("simulated create error"))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := gomega.NewWithT(t)

			rr := tc.setupRR(t)
			ctx := t.Context()

			err := dashboardctrl.ReconcileHardwareProfiles(ctx, rr)

			if tc.expectError {
				g.Expect(err).Should(gomega.HaveOccurred())
			} else {
				g.Expect(err).ShouldNot(gomega.HaveOccurred())
			}

			tc.validate(t, rr)
		})
	}
}

// Test dashboardctrl.ProcessHardwareProfile function directly.
func TestProcessHardwareProfile(t *testing.T) {
	t.Run("SuccessfulProcessing", testProcessHardwareProfileSuccessful)
	t.Run("ConversionError", testProcessHardwareProfileConversionError)
	t.Run("CreateNewProfile", testProcessHardwareProfileCreateNew)
	t.Run("UpdateExistingProfile", testProcessHardwareProfileUpdateExisting)
	t.Run("GetError", testProcessHardwareProfileGetError)
}

func testProcessHardwareProfileSuccessful(t *testing.T) {
	// Create a mock dashboard hardware profile
	dashboardHWP := createDashboardHWP(t, TestProfile, true, "gpu")

	// Create an existing infrastructure hardware profile
	existingInfraHWP := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  TestDisplayName,
					Identifier:   "gpu",
					MinCount:     intstr.FromInt32(1),
					DefaultCount: intstr.FromInt32(1),
					ResourceType: "Accelerator",
				},
			},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(existingInfraHWP))
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *dashboardHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())
}

func testProcessHardwareProfileConversionError(t *testing.T) {
	// Create a mock dashboard hardware profile with invalid spec
	dashboardHWP := &unstructured.Unstructured{}
	dashboardHWP.SetGroupVersionKind(gvk.DashboardHardwareProfile)
	dashboardHWP.SetName(TestProfile)
	dashboardHWP.SetNamespace(TestNamespace)
	dashboardHWP.Object["spec"] = "invalid-spec" // Invalid spec type

	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *dashboardHWP)
	gomega.NewWithT(t).Expect(err).Should(gomega.HaveOccurred())
	gomega.NewWithT(t).Expect(err.Error()).Should(gomega.ContainSubstring("failed to convert dashboard hardware profile"))
}

func testProcessHardwareProfileCreateNew(t *testing.T) {
	// Create a mock dashboard hardware profile
	dashboardHWP := createDashboardHWP(t, TestProfile, true, "gpu")

	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *dashboardHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())
}

func testProcessHardwareProfileUpdateExisting(t *testing.T) {
	// Create a mock dashboard hardware profile with different specs
	dashboardHWP := createDashboardHWP(t, "updated-profile", true, "cpu")

	// Create an existing infrastructure hardware profile
	existingInfraHWP := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "updated-profile",
			Namespace: TestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  "Updated Profile",
					Identifier:   "cpu",
					MinCount:     intstr.FromInt32(1),
					DefaultCount: intstr.FromInt32(1),
					ResourceType: "CPU",
				},
			},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(existingInfraHWP))
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	// This should update the existing profile
	err = dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *dashboardHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())
}

func testProcessHardwareProfileGetError(t *testing.T) {
	// Create a mock dashboard hardware profile
	dashboardHWP := createDashboardHWP(t, TestProfile, true, "gpu")

	// Create a mock client that returns a controlled Get error
	expectedError := errors.New("mock client Get error")
	mockClient, err := fakeclient.New(
		fakeclient.WithObjects(dashboardHWP),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return expectedError
			},
		}),
	)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = mockClient

	ctx := t.Context()
	logger := log.FromContext(ctx)

	// This should return the expected error from the mock client
	processErr := dashboardctrl.ProcessHardwareProfile(ctx, rr, logger, *dashboardHWP)
	gomega.NewWithT(t).Expect(processErr).Should(gomega.HaveOccurred())
	gomega.NewWithT(t).Expect(processErr.Error()).Should(gomega.ContainSubstring("failed to get infrastructure hardware profile"))
	gomega.NewWithT(t).Expect(processErr.Error()).Should(gomega.ContainSubstring(expectedError.Error()))
}

// Test dashboardctrl.CreateInfraHWP function directly.
func TestCreateInfraHWP(t *testing.T) {
	t.Run("SuccessfulCreation", testCreateInfraHWPSuccessful)
	t.Run("WithAnnotations", testCreateInfraHWPWithAnnotations)
	t.Run("WithTolerations", testCreateInfraHWPWithTolerations)
	t.Run("WithIdentifiers", testCreateInfraHWPWithIdentifiers)
}

func testCreateInfraHWPSuccessful(t *testing.T) {
	dashboardHWP := &dashboardctrl.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: dashboardctrl.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
			NodeSelector: map[string]string{
				NodeTypeKey: "gpu",
			},
		},
	}

	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.CreateInfraHWP(ctx, rr, logger, dashboardHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify the created InfrastructureHardwareProfile
	var infraHWP infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: TestProfile, Namespace: TestNamespace}, &infraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Assert the created object's fields match expectations
	gomega.NewWithT(t).Expect(infraHWP.Annotations[displayNameKey]).Should(gomega.Equal(TestDisplayName))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[descriptionKey]).Should(gomega.Equal(TestDescription))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[disabledKey]).Should(gomega.Equal("false"))
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.NodeSelector).Should(gomega.Equal(map[string]string{NodeTypeKey: "gpu"}))
}

func testCreateInfraHWPWithAnnotations(t *testing.T) {
	dashboardHWP := &dashboardctrl.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
			Annotations: map[string]string{
				customKey: customValue,
			},
		},
		Spec: dashboardctrl.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
			NodeSelector: map[string]string{
				NodeTypeKey: "gpu",
			},
		},
	}

	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.CreateInfraHWP(ctx, rr, logger, dashboardHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify the created InfrastructureHardwareProfile
	var infraHWP infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: TestProfile, Namespace: TestNamespace}, &infraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Assert the created object's fields match expectations
	gomega.NewWithT(t).Expect(infraHWP.Annotations[displayNameKey]).Should(gomega.Equal(TestDisplayName))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[descriptionKey]).Should(gomega.Equal(TestDescription))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[disabledKey]).Should(gomega.Equal("false"))
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.NodeSelector).Should(gomega.Equal(map[string]string{NodeTypeKey: "gpu"}))

	// Assert the custom annotation exists and equals customValue
	gomega.NewWithT(t).Expect(infraHWP.Annotations[customKey]).Should(gomega.Equal(customValue))
}

func testCreateInfraHWPWithTolerations(t *testing.T) {
	dashboardHWP := &dashboardctrl.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: dashboardctrl.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
			NodeSelector: map[string]string{
				NodeTypeKey: "gpu",
			},
			Tolerations: []corev1.Toleration{
				{
					Key:    NvidiaGPUKey,
					Value:  "true",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.CreateInfraHWP(ctx, rr, logger, dashboardHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify the created InfrastructureHardwareProfile
	var infraHWP infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: TestProfile, Namespace: TestNamespace}, &infraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Assert the created object's fields match expectations
	gomega.NewWithT(t).Expect(infraHWP.Annotations[displayNameKey]).Should(gomega.Equal(TestDisplayName))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[descriptionKey]).Should(gomega.Equal(TestDescription))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[disabledKey]).Should(gomega.Equal("false"))
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.NodeSelector).Should(gomega.Equal(map[string]string{NodeTypeKey: "gpu"}))

	// Assert the toleration with key NvidiaGPUKey/value "true" and effect NoSchedule is present
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.Tolerations).Should(gomega.HaveLen(1))
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.Tolerations[0].Key).Should(gomega.Equal(NvidiaGPUKey))
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.Tolerations[0].Value).Should(gomega.Equal("true"))
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.Tolerations[0].Effect).Should(gomega.Equal(corev1.TaintEffectNoSchedule))
}

func testCreateInfraHWPWithIdentifiers(t *testing.T) {
	dashboardHWP := &dashboardctrl.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: dashboardctrl.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
			NodeSelector: map[string]string{
				NodeTypeKey: "gpu",
			},
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  "GPU",
					Identifier:   NvidiaGPUKey,
					ResourceType: "Accelerator",
				},
			},
		},
	}

	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.CreateInfraHWP(ctx, rr, logger, dashboardHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify the created InfrastructureHardwareProfile
	var infraHWP infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: TestProfile, Namespace: TestNamespace}, &infraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Assert the created object's fields match expectations
	gomega.NewWithT(t).Expect(infraHWP.Annotations[displayNameKey]).Should(gomega.Equal(TestDisplayName))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[descriptionKey]).Should(gomega.Equal(TestDescription))
	gomega.NewWithT(t).Expect(infraHWP.Annotations[disabledKey]).Should(gomega.Equal("false"))
	gomega.NewWithT(t).Expect(infraHWP.Spec.SchedulingSpec.Node.NodeSelector).Should(gomega.Equal(map[string]string{NodeTypeKey: "gpu"}))

	// Assert the identifiers slice contains the expected HardwareIdentifier
	gomega.NewWithT(t).Expect(infraHWP.Spec.Identifiers).Should(gomega.HaveLen(1))
	gomega.NewWithT(t).Expect(infraHWP.Spec.Identifiers[0].DisplayName).Should(gomega.Equal("GPU"))
	gomega.NewWithT(t).Expect(infraHWP.Spec.Identifiers[0].Identifier).Should(gomega.Equal(NvidiaGPUKey))
	gomega.NewWithT(t).Expect(infraHWP.Spec.Identifiers[0].ResourceType).Should(gomega.Equal("Accelerator"))
}

// Test dashboardctrl.UpdateInfraHWP function directly.
func TestUpdateInfraHWP(t *testing.T) {
	t.Run("SuccessfulUpdate", testUpdateInfraHWPSuccessful)
	t.Run("WithNilAnnotations", testUpdateInfraHWPWithNilAnnotations)
	t.Run("WithExistingAnnotations", testUpdateInfraHWPWithExistingAnnotations)
}

func testUpdateInfraHWPSuccessful(t *testing.T) {
	dashboardHWP := &dashboardctrl.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: dashboardctrl.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
			NodeSelector: map[string]string{
				NodeTypeKey: "gpu",
			},
		},
	}

	infraHWP := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  TestDisplayName,
					Identifier:   "gpu",
					MinCount:     intstr.FromInt32(1),
					DefaultCount: intstr.FromInt32(1),
					ResourceType: "Accelerator",
				},
			},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(infraHWP))
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.UpdateInfraHWP(ctx, rr, logger, dashboardHWP, infraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Fetch the updated HardwareProfile from the fake client
	var updatedInfraHWP infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: TestProfile, Namespace: TestNamespace}, &updatedInfraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Assert spec fields reflect the dashboardctrl.DashboardHardwareProfile changes
	gomega.NewWithT(t).Expect(updatedInfraHWP.Spec.SchedulingSpec.Node.NodeSelector).Should(gomega.Equal(dashboardHWP.Spec.NodeSelector))

	// Assert annotations were properly set
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(displayNameKey, TestDisplayName))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(descriptionKey, TestDescription))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(disabledKey, "false"))

	// Assert resource metadata remains correct
	gomega.NewWithT(t).Expect(updatedInfraHWP.Name).Should(gomega.Equal(TestProfile))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Namespace).Should(gomega.Equal(TestNamespace))
}

func testUpdateInfraHWPWithNilAnnotations(t *testing.T) {
	dashboardHWP := &dashboardctrl.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: dashboardctrl.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
			NodeSelector: map[string]string{
				NodeTypeKey: "gpu",
			},
		},
	}

	infraHWP := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  TestDisplayName,
					Identifier:   "gpu",
					MinCount:     intstr.FromInt32(1),
					DefaultCount: intstr.FromInt32(1),
					ResourceType: "Accelerator",
				},
			},
		},
	}
	infraHWP.Annotations = nil // Test nil annotations case

	cli, err := fakeclient.New(fakeclient.WithObjects(infraHWP))
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.UpdateInfraHWP(ctx, rr, logger, dashboardHWP, infraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Fetch the updated HardwareProfile from the fake client
	var updatedInfraHWP infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: TestProfile, Namespace: TestNamespace}, &updatedInfraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Assert spec fields reflect the dashboardctrl.DashboardHardwareProfile changes
	gomega.NewWithT(t).Expect(updatedInfraHWP.Spec.SchedulingSpec.Node.NodeSelector).Should(gomega.Equal(dashboardHWP.Spec.NodeSelector))

	// Assert annotations were properly set (nil annotations become the dashboard annotations)
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).ShouldNot(gomega.BeNil())
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(displayNameKey, TestDisplayName))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(descriptionKey, TestDescription))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(disabledKey, "false"))

	// Assert resource metadata remains correct
	gomega.NewWithT(t).Expect(updatedInfraHWP.Name).Should(gomega.Equal(TestProfile))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Namespace).Should(gomega.Equal(TestNamespace))
}

func testUpdateInfraHWPWithExistingAnnotations(t *testing.T) {
	dashboardHWP := &dashboardctrl.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
			Annotations: map[string]string{
				customKey: customValue,
			},
		},
		Spec: dashboardctrl.DashboardHardwareProfileSpec{
			DisplayName: TestDisplayName,
			Enabled:     true,
			Description: TestDescription,
			NodeSelector: map[string]string{
				NodeTypeKey: "gpu",
			},
		},
	}

	infraHWP := &infrav1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestProfile,
			Namespace: TestNamespace,
			Annotations: map[string]string{
				"existing-annotation": "existing-value",
			},
		},
		Spec: infrav1.HardwareProfileSpec{
			Identifiers: []infrav1.HardwareIdentifier{
				{
					DisplayName:  TestDisplayName,
					Identifier:   "gpu",
					MinCount:     intstr.FromInt32(1),
					DefaultCount: intstr.FromInt32(1),
					ResourceType: "Accelerator",
				},
			},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(infraHWP))
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := SetupTestReconciliationRequestSimple(t)
	rr.Client = cli

	ctx := t.Context()
	logger := log.FromContext(ctx)

	err = dashboardctrl.UpdateInfraHWP(ctx, rr, logger, dashboardHWP, infraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Fetch the updated HardwareProfile from the fake client
	var updatedInfraHWP infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{Name: TestProfile, Namespace: TestNamespace}, &updatedInfraHWP)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Assert spec fields reflect the dashboardctrl.DashboardHardwareProfile changes
	gomega.NewWithT(t).Expect(updatedInfraHWP.Spec.SchedulingSpec.Node.NodeSelector).Should(gomega.Equal(dashboardHWP.Spec.NodeSelector))

	// Assert annotations were properly merged (existing annotations preserved and merged with dashboard annotations)
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(displayNameKey, TestDisplayName))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(descriptionKey, TestDescription))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(disabledKey, "false"))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue("existing-annotation", "existing-value"))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Annotations).Should(gomega.HaveKeyWithValue(customKey, customValue))

	// Assert resource metadata remains correct
	gomega.NewWithT(t).Expect(updatedInfraHWP.Name).Should(gomega.Equal(TestProfile))
	gomega.NewWithT(t).Expect(updatedInfraHWP.Namespace).Should(gomega.Equal(TestNamespace))
}
