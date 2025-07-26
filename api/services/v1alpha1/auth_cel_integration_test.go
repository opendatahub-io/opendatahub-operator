//go:build integration

package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// TestAuthCELValidationIntegration is the main integration test suite that validates
// CEL (Common Expression Language) validation rules are properly enforced by a real
// Kubernetes API server. This is critical for security because:
//
// 1. CEL validation rules are embedded in the Auth CRD and enforced at the API level
// 2. This provides the first line of defense against invalid/insecure configurations
// 3. Unlike fake clients, envtest uses a real API server that enforces CEL rules
//
// The test suite covers three main scenarios:
// - Creation validation: Prevents invalid Auth resources from being created
// - Update validation: Prevents existing Auth resources from being corrupted
// - Valid resource acceptance: Ensures legitimate configurations are allowed
//
// Security Rules Tested:
// - BLOCK: system:authenticated in AdminGroups (major security risk)
// - BLOCK: Empty strings in any group (invalid configuration)
// - ALLOW: system:authenticated in AllowedGroups (valid use case)
// - ALLOW: Empty arrays (no groups specified)
func TestAuthCELValidationIntegration(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	g := NewWithT(t)
	ctx := context.Background()

	projectDir, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())

	// Set up the envtest environment
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(projectDir, "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())

	// Ensure we stop the environment when done
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	// Set up the client with proper scheme
	k8sClient, err := client.New(cfg, client.Options{Scheme: setupScheme()})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(k8sClient).ToNot(BeNil())

	// Run the three main test scenarios
	t.Run("CEL validations block invalid Auth creation", func(t *testing.T) {
		testAuthCELValidationCreation(t, ctx, k8sClient)
	})

	t.Run("CEL validations block invalid Auth updates", func(t *testing.T) {
		testAuthCELValidationUpdate(t, ctx, k8sClient)
	})

	t.Run("CEL validations allow valid Auth resources", func(t *testing.T) {
		testAuthCELValidationValid(t, ctx, k8sClient)
	})
}

func setupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = SchemeBuilder.AddToScheme(s)
	return s
}

// testAuthCELValidationCreation validates that CEL rules prevent creation of invalid Auth resources.
// This is the primary security test ensuring that dangerous configurations like giving
// admin access to all authenticated users cannot be created.
func testAuthCELValidationCreation(t *testing.T, ctx context.Context, k8sClient client.Client) {
	g := NewWithT(t)

	testCases := []struct {
		name          string
		auth          *Auth
		expectError   bool
		errorContains string
	}{
		{
			name: "invalid auth with system:authenticated in AdminGroups",
			auth: &Auth{
				ObjectMeta: metav1.ObjectMeta{
					Name: "auth", // Must be "auth" due to singleton validation
				},
				Spec: AuthSpec{
					AdminGroups:   []string{"valid-admin-group", "system:authenticated"},
					AllowedGroups: []string{"valid-allowed-group"},
				},
			},
			expectError:   true,
			errorContains: "AdminGroups cannot contain 'system:authenticated'",
		},
		{
			name: "invalid auth with empty string in AdminGroups",
			auth: &Auth{
				ObjectMeta: metav1.ObjectMeta{
					Name: "auth", // Must be "auth" due to singleton validation
				},
				Spec: AuthSpec{
					AdminGroups:   []string{"valid-admin-group", ""},
					AllowedGroups: []string{"valid-allowed-group"},
				},
			},
			expectError:   true,
			errorContains: "AdminGroups cannot contain",
		},
		{
			name: "invalid auth with empty string in AllowedGroups",
			auth: &Auth{
				ObjectMeta: metav1.ObjectMeta{
					Name: "auth", // Must be "auth" due to singleton validation
				},
				Spec: AuthSpec{
					AdminGroups:   []string{"valid-admin-group"},
					AllowedGroups: []string{"valid-allowed-group", ""},
				},
			},
			expectError:   true,
			errorContains: "AllowedGroups cannot contain empty strings",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			err := k8sClient.Create(ctx, tc.auth)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), "Expected CEL validation to fail")
				g.Expect(err.Error()).To(ContainSubstring(tc.errorContains),
					"Error message should contain expected text")
			} else {
				g.Expect(err).ToNot(HaveOccurred(), "Expected CEL validation to pass")
				// Clean up
				_ = k8sClient.Delete(ctx, tc.auth)
			}
		})
	}
}

// testAuthCELValidationUpdate validates that CEL rules prevent updates that would make
// existing Auth resources invalid. This is important because it prevents configuration
// drift and ensures existing resources cannot be corrupted.
func testAuthCELValidationUpdate(t *testing.T, ctx context.Context, k8sClient client.Client) {
	g := NewWithT(t)

	// Create a valid Auth resource first
	validAuth := &Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: "auth", // Must be "auth" due to singleton validation
		},
		Spec: AuthSpec{
			AdminGroups:   []string{"valid-admin-group"},
			AllowedGroups: []string{"valid-allowed-group"},
		},
	}

	err := k8sClient.Create(ctx, validAuth)
	g.Expect(err).ToNot(HaveOccurred())

	// Ensure cleanup
	defer func() {
		_ = k8sClient.Delete(ctx, validAuth)
	}()

	testCases := []struct {
		name          string
		updateSpec    AuthSpec
		expectError   bool
		errorContains string
	}{
		{
			name: "update to invalid AdminGroups with system:authenticated",
			updateSpec: AuthSpec{
				AdminGroups:   []string{"valid-admin-group", "system:authenticated"},
				AllowedGroups: []string{"valid-allowed-group"},
			},
			expectError:   true,
			errorContains: "AdminGroups cannot contain 'system:authenticated'",
		},
		{
			name: "update to invalid AdminGroups with empty string",
			updateSpec: AuthSpec{
				AdminGroups:   []string{"valid-admin-group", ""},
				AllowedGroups: []string{"valid-allowed-group"},
			},
			expectError:   true,
			errorContains: "AdminGroups cannot contain",
		},
		{
			name: "update to invalid AllowedGroups with empty string",
			updateSpec: AuthSpec{
				AdminGroups:   []string{"valid-admin-group"},
				AllowedGroups: []string{"valid-allowed-group", ""},
			},
			expectError:   true,
			errorContains: "AllowedGroups cannot contain empty strings",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			// Get the current Auth resource
			currentAuth := &Auth{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "auth"}, currentAuth) // Must be "auth"
			g.Expect(err).ToNot(HaveOccurred())

			// Update the spec
			currentAuth.Spec = tc.updateSpec

			// Try to update the Auth resource
			err = k8sClient.Update(ctx, currentAuth)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), "Expected CEL validation to fail on update")
				g.Expect(err.Error()).To(ContainSubstring(tc.errorContains),
					"Error message should contain expected text")
			} else {
				g.Expect(err).ToNot(HaveOccurred(), "Expected CEL validation to pass on update")
			}
		})
	}
}

// testAuthCELValidationValid validates that CEL rules allow legitimate Auth configurations.
// This is equally important as blocking invalid configs - we must ensure that valid
// use cases continue to work properly.
func testAuthCELValidationValid(t *testing.T, ctx context.Context, k8sClient client.Client) {
	g := NewWithT(t)

	testCases := []struct {
		name string
		spec AuthSpec
	}{
		{
			name: "valid auth with proper groups",
			spec: AuthSpec{
				AdminGroups:   []string{"valid-admin-group", "another-admin-group"},
				AllowedGroups: []string{"valid-allowed-group", "system:authenticated"},
			},
		},
		{
			name: "valid auth with system:authenticated in AllowedGroups only",
			spec: AuthSpec{
				AdminGroups:   []string{"valid-admin-group"},
				AllowedGroups: []string{"system:authenticated", "another-group"},
			},
		},
		{
			name: "valid auth with empty arrays",
			spec: AuthSpec{
				AdminGroups:   []string{},
				AllowedGroups: []string{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			auth := &Auth{
				ObjectMeta: metav1.ObjectMeta{
					Name: "auth", // Must be "auth" due to singleton validation
				},
				Spec: tc.spec,
			}

			err := k8sClient.Create(ctx, auth)
			g.Expect(err).ToNot(HaveOccurred(), "Expected CEL validation to pass for valid Auth")

			// Verify the resource was created
			created := &Auth{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "auth"}, created)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(created.Spec.AdminGroups).To(Equal(tc.spec.AdminGroups))
			g.Expect(created.Spec.AllowedGroups).To(Equal(tc.spec.AllowedGroups))

			// Clean up after each test
			err = k8sClient.Delete(ctx, auth)
			g.Expect(err).ToNot(HaveOccurred())
		})
	}
}
