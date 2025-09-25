// This file contains tests for dashboard controller dependencies functionality.
// These tests verify the dashboard.ConfigureDependencies function and related dependency logic.
package dashboard_test

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard/dashboard_test"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestConfigureDependenciesBasicCases(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name              string
		setupRR           func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest
		expectError       bool
		expectPanic       bool
		errorContains     string
		expectedResources int
		validateResult    func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "OpenDataHub",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := dashboard_test.CreateTestDashboard()
				dsci := dashboard_test.CreateTestDSCI()
				return dashboard_test.CreateTestReconciliationRequest(cli, dashboardInstance, dsci, common.Release{Name: cluster.OpenDataHub})
			},
			expectError:       false,
			expectedResources: 0,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).Should(BeEmpty())
			},
		},
		{
			name: "SelfManagedRhoai",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := dashboard_test.CreateTestDashboard()
				dsci := dashboard_test.CreateTestDSCI()
				return dashboard_test.CreateTestReconciliationRequest(cli, dashboardInstance, dsci, common.Release{Name: cluster.SelfManagedRhoai})
			},
			expectError:       false,
			expectedResources: 1,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				secret := rr.Resources[0]
				g.Expect(secret.GetKind()).Should(Equal("Secret"))
				g.Expect(secret.GetName()).Should(Equal(dashboard.AnacondaSecretName))
				g.Expect(secret.GetNamespace()).Should(Equal(dashboard_test.TestNamespace))
			},
		},
		{
			name: "ManagedRhoai",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := &componentApi.Dashboard{}
				dsci := &dsciv1.DSCInitialization{
					Spec: dsciv1.DSCInitializationSpec{
						ApplicationsNamespace: dashboard_test.TestNamespace,
					},
				}
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.ManagedRhoai},
				}
			},
			expectError:       false,
			expectedResources: 1,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources[0].GetName()).Should(Equal(dashboard.AnacondaSecretName))
				g.Expect(rr.Resources[0].GetNamespace()).Should(Equal(dashboard_test.TestNamespace))
			},
		},
		{
			name: "WithEmptyNamespace",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := &componentApi.Dashboard{}
				dsci := &dsciv1.DSCInitialization{
					Spec: dsciv1.DSCInitializationSpec{
						ApplicationsNamespace: "", // Empty namespace
					},
				}
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:       true,
			errorContains:     "namespace cannot be empty",
			expectedResources: 0,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).Should(BeEmpty())
			},
		},
		{
			name: "SecretProperties",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := dashboard_test.CreateTestDashboard()
				dsci := dashboard_test.CreateTestDSCI()
				return dashboard_test.CreateTestReconciliationRequest(cli, dashboardInstance, dsci, common.Release{Name: cluster.SelfManagedRhoai})
			},
			expectError:       false,
			expectedResources: 1,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				validateSecretProperties(t, &rr.Resources[0], dashboard.AnacondaSecretName, dashboard_test.TestNamespace)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cli := dashboard_test.CreateTestClient(t)
			rr := tc.setupRR(cli, ctx)
			runDependencyTest(t, ctx, tc, rr)
		})
	}
}

func TestConfigureDependenciesErrorCases(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name              string
		setupRR           func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest
		expectError       bool
		expectPanic       bool
		errorContains     string
		expectedResources int
		validateResult    func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "NilDSCI",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := dashboard_test.CreateTestDashboard()
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					DSCI:     nil, // Nil DSCI
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:       true,
			errorContains:     "DSCI cannot be nil",
			expectedResources: 0,
		},
		{
			name: "SpecialCharactersInNamespace",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := dashboard_test.CreateTestDashboard()
				dsci := &dsciv1.DSCInitialization{
					Spec: dsciv1.DSCInitializationSpec{
						ApplicationsNamespace: "test-namespace-with-special-chars!@#$%",
					},
				}
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:       true,
			errorContains:     "must be lowercase and conform to RFC1123 DNS label rules",
			expectedResources: 0,
		},
		{
			name: "LongNamespace",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := dashboard_test.CreateTestDashboard()
				longNamespace := strings.Repeat("a", 1000)
				dsci := &dsciv1.DSCInitialization{
					Spec: dsciv1.DSCInitializationSpec{
						ApplicationsNamespace: longNamespace,
					},
				}
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:       true,
			errorContains:     "exceeds maximum length of 63 characters",
			expectedResources: 0,
		},
		{
			name: "NilClient",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := dashboard_test.CreateTestDashboard()
				dsci := dashboard_test.CreateTestDSCI()
				return &odhtypes.ReconciliationRequest{
					Client:   nil, // Nil client
					Instance: dashboardInstance,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:       true,
			errorContains:     "client cannot be nil",
			expectedResources: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cli := dashboard_test.CreateTestClient(t)
			rr := tc.setupRR(cli, ctx)
			runDependencyTest(t, ctx, tc, rr)
		})
	}
}

// runDependencyTest executes a single dependency test case.
func runDependencyTest(t *testing.T, ctx context.Context, tc struct {
	name              string
	setupRR           func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest
	expectError       bool
	expectPanic       bool
	errorContains     string
	expectedResources int
	validateResult    func(t *testing.T, rr *odhtypes.ReconciliationRequest)
}, rr *odhtypes.ReconciliationRequest) {
	t.Helper()
	g := NewWithT(t)

	if tc.expectPanic {
		dashboard_test.AssertPanics(t, func() {
			_ = dashboard.ConfigureDependencies(ctx, rr)
		}, "dashboard.ConfigureDependencies should panic")
		return
	}

	err := dashboard.ConfigureDependencies(ctx, rr)

	if tc.expectError {
		g.Expect(err).Should(HaveOccurred())
		if tc.errorContains != "" {
			g.Expect(err.Error()).Should(ContainSubstring(tc.errorContains))
		}
	} else {
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(HaveLen(tc.expectedResources))
	}

	if tc.validateResult != nil {
		tc.validateResult(t, rr)
	}
}

// validateSecretProperties validates secret properties for the specific test case.
func validateSecretProperties(t *testing.T, secret *unstructured.Unstructured, expectedName, expectedNamespace string) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(secret.GetAPIVersion()).Should(Equal("v1"))
	g.Expect(secret.GetKind()).Should(Equal("Secret"))
	g.Expect(secret.GetName()).Should(Equal(expectedName))
	g.Expect(secret.GetNamespace()).Should(Equal(expectedNamespace))

	// Check the type field in the object
	secretType, found, err := unstructured.NestedString(secret.Object, "type")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(secretType).Should(Equal("Opaque"))
}
