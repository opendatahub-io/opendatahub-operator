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
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

const resourcesNotEmptyMsg = "Resources slice should not be empty"

// createTestRR creates a reconciliation request with the specified namespace.
func createTestRR(namespace string) func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
	return func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
		// Create DSCI resource
		dsci := CreateTestDSCI(namespace)
		_ = cli.Create(ctx, dsci) // Ignore error - test will catch it later if needed

		dashboardInstance := CreateTestDashboard()
		return &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: dashboardInstance,
			Release:  common.Release{Name: cluster.SelfManagedRhoai},
		}
	}
}

func TestConfigureDependenciesBasicCases(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		setupRR        func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest
		expectError    bool
		expectPanic    bool
		errorContains  string
		validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "OpenDataHub",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := CreateTestDashboard()
				return CreateTestReconciliationRequest(cli, dashboardInstance, common.Release{Name: cluster.OpenDataHub})
			},
			expectError: false,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).Should(BeEmpty())
			},
		},
		{
			name: "SelfManagedRhoai",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				// Create DSCI resource
				dsci := CreateTestDSCI(TestNamespace)
				_ = cli.Create(ctx, dsci) // Ignore error - test will catch it later if needed

				dashboardInstance := CreateTestDashboard()
				return CreateTestReconciliationRequest(cli, dashboardInstance, common.Release{Name: cluster.SelfManagedRhoai})
			},
			expectError: false,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).ShouldNot(BeEmpty(), resourcesNotEmptyMsg)
				secret := rr.Resources[0]
				g.Expect(secret.GetKind()).Should(Equal("Secret"))
				g.Expect(secret.GetName()).Should(Equal(dashboard.AnacondaSecretName))
				g.Expect(secret.GetNamespace()).Should(Equal(TestNamespace))
			},
		},
		{
			name: "ManagedRhoai",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				// Create DSCI resource
				dsci := CreateTestDSCI(TestNamespace)
				_ = cli.Create(ctx, dsci) // Ignore error - test will catch it later if needed

				dashboardInstance := CreateTestDashboard()
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					Release:  common.Release{Name: cluster.ManagedRhoai},
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).ShouldNot(BeEmpty(), resourcesNotEmptyMsg)
				g.Expect(rr.Resources[0].GetName()).Should(Equal(dashboard.AnacondaSecretName))
				g.Expect(rr.Resources[0].GetNamespace()).Should(Equal(TestNamespace))
			},
		},
		{
			name: "WithEmptyNamespace",
			// Empty namespace
			setupRR:       createTestRR(""),
			expectError:   false,
			errorContains: "",
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).Should(HaveLen(1))
				g.Expect(rr.Resources).ShouldNot(BeEmpty(), resourcesNotEmptyMsg)
				g.Expect(rr.Resources[0].GetName()).Should(Equal(dashboard.AnacondaSecretName))
				g.Expect(rr.Resources[0].GetNamespace()).Should(Equal(""))
			},
		},
		{
			name: "SecretProperties",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				// Create DSCI resource
				dsci := CreateTestDSCI(TestNamespace)
				_ = cli.Create(ctx, dsci) // Ignore error - test will catch it later if needed

				dashboardInstance := CreateTestDashboard()
				return CreateTestReconciliationRequest(cli, dashboardInstance, common.Release{Name: cluster.SelfManagedRhoai})
			},
			expectError: false,
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).ShouldNot(BeEmpty(), resourcesNotEmptyMsg)
				validateSecretProperties(t, &rr.Resources[0], dashboard.AnacondaSecretName, TestNamespace)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cli := CreateTestClient(t)
			rr := tc.setupRR(cli, ctx)
			runDependencyTest(t, ctx, tc, rr)
		})
	}
}

func TestConfigureDependenciesErrorCases(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		setupRR        func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest
		expectError    bool
		expectPanic    bool
		errorContains  string
		validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name: "NilDSCI",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := CreateTestDashboard()
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:   true,
			errorContains: "DSCI not found",
		},
		{
			name:          "SpecialCharactersInNamespace",
			setupRR:       createTestRR("test-namespace-with-special-chars!@#$%"),
			expectError:   false,
			errorContains: "",
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).Should(HaveLen(1))
				g.Expect(rr.Resources[0].GetName()).Should(Equal(dashboard.AnacondaSecretName))
				g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("test-namespace-with-special-chars!@#$%"))
			},
		},
		{
			name: "LongNamespace",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				// Create DSCI resource
				dsci := CreateTestDSCI(strings.Repeat("a", 1000))
				_ = cli.Create(ctx, dsci) // Ignore error - test will catch it later if needed

				dashboardInstance := CreateTestDashboard()
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: dashboardInstance,
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:   false,
			errorContains: "",
			validateResult: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).Should(HaveLen(1))
				g.Expect(rr.Resources).ShouldNot(BeEmpty(), resourcesNotEmptyMsg)
				g.Expect(rr.Resources[0].GetName()).Should(Equal(dashboard.AnacondaSecretName))
				g.Expect(rr.Resources[0].GetNamespace()).Should(Equal(strings.Repeat("a", 1000)))
			},
		},
		{
			name: "NilClient",
			setupRR: func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest {
				dashboardInstance := CreateTestDashboard()
				return &odhtypes.ReconciliationRequest{
					Client:   nil, // Nil client
					Instance: dashboardInstance,
					Release:  common.Release{Name: cluster.SelfManagedRhoai},
				}
			},
			expectError:   true,
			errorContains: "client cannot be nil",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cli := CreateTestClient(t)
			rr := tc.setupRR(cli, ctx)
			runDependencyTest(t, ctx, tc, rr)
		})
	}
}

// runDependencyTest executes a single dependency test case.
func runDependencyTest(t *testing.T, ctx context.Context, tc struct {
	name           string
	setupRR        func(cli client.Client, ctx context.Context) *odhtypes.ReconciliationRequest
	expectError    bool
	expectPanic    bool
	errorContains  string
	validateResult func(t *testing.T, rr *odhtypes.ReconciliationRequest)
}, rr *odhtypes.ReconciliationRequest) {
	t.Helper()
	g := NewWithT(t)

	if tc.expectPanic {
		AssertPanics(t, func() {
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
