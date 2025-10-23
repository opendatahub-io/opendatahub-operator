package cluster_test

import (
	"encoding/base64"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// TestNewSecretWithRandomType tests the creation of a random-type secret.
func TestNewSecretWithRandomType(t *testing.T) {
	testCases := []struct {
		name       string
		secretName string
		secretType string
		complexity int
	}{
		{
			name:       "Small random secret",
			secretName: "test-secret",
			secretType: "random",
			complexity: 8,
		},
		{
			name:       "Medium random secret",
			secretName: "client-secret",
			secretType: "random",
			complexity: 24,
		},
		{
			name:       "Large random secret",
			secretName: "large-secret",
			secretType: "random",
			complexity: 64,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret, err := cluster.NewSecret(tc.secretName, tc.secretType, tc.complexity)

			// Check for errors
			if err != nil {
				t.Fatalf("NewSecret returned unexpected error: %v", err)
			}

			// Check secret properties
			if secret == nil {
				t.Fatal("NewSecret returned nil secret")
			}

			if secret.Name != tc.secretName {
				t.Errorf("Expected secret name %q, got %q", tc.secretName, secret.Name)
			}

			if secret.Type != tc.secretType {
				t.Errorf("Expected secret type %q, got %q", tc.secretType, secret.Type)
			}

			if secret.Complexity != tc.complexity {
				t.Errorf("Expected complexity %d, got %d", tc.complexity, secret.Complexity)
			}

			// Check that value was generated
			if secret.Value == "" {
				t.Error("Expected non-empty secret value")
			}

			// Check that value has correct length
			if len(secret.Value) != tc.complexity {
				t.Errorf("Expected value length %d, got %d", tc.complexity, len(secret.Value))
			}

			// Check that value contains only valid characters (from LetterRunes)
			for i, char := range secret.Value {
				found := false
				for _, validChar := range cluster.LetterRunes {
					if char == validChar {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Invalid character %q at position %d in secret value", char, i)
				}
			}
		})
	}
}

// TestNewSecretWithOAuthType tests the creation of an oauth-type secret.
func TestNewSecretWithOAuthType(t *testing.T) {
	testCases := []struct {
		name       string
		secretName string
		secretType string
		complexity int
	}{
		{
			name:       "Small OAuth secret",
			secretName: "oauth-secret",
			secretType: "oauth",
			complexity: 16,
		},
		{
			name:       "Medium OAuth secret",
			secretName: "oauth-client-secret",
			secretType: "oauth",
			complexity: 32,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret, err := cluster.NewSecret(tc.secretName, tc.secretType, tc.complexity)

			// Check for errors
			if err != nil {
				t.Fatalf("NewSecret returned unexpected error: %v", err)
			}

			// Check secret properties
			if secret == nil {
				t.Fatal("NewSecret returned nil secret")
			}

			if secret.Name != tc.secretName {
				t.Errorf("Expected secret name %q, got %q", tc.secretName, secret.Name)
			}

			if secret.Type != tc.secretType {
				t.Errorf("Expected secret type %q, got %q", tc.secretType, secret.Type)
			}

			if secret.Complexity != tc.complexity {
				t.Errorf("Expected complexity %d, got %d", tc.complexity, secret.Complexity)
			}

			// Check that value was generated
			if secret.Value == "" {
				t.Error("Expected non-empty secret value")
			}

			// Verify it's double base64 encoded
			// First decode
			firstDecode, err := base64.StdEncoding.DecodeString(secret.Value)
			if err != nil {
				t.Errorf("Failed to decode first layer of base64: %v", err)
			}

			// Second decode should also work (double encoding)
			secondDecode, err := base64.StdEncoding.DecodeString(string(firstDecode))
			if err != nil {
				t.Errorf("Failed to decode second layer of base64: %v", err)
			}

			// The final decoded value should have the original complexity length
			if len(secondDecode) != tc.complexity {
				t.Errorf("Expected decoded value length %d, got %d", tc.complexity, len(secondDecode))
			}
		})
	}
}

// TestNewSecretWithInvalidType tests error handling for invalid secret types.
func TestNewSecretWithInvalidType(t *testing.T) {
	testCases := []struct {
		name       string
		secretName string
		secretType string
		complexity int
	}{
		{
			name:       "Invalid type 'invalid'",
			secretName: "test-secret",
			secretType: "invalid",
			complexity: 16,
		},
		{
			name:       "Empty type",
			secretName: "test-secret",
			secretType: "",
			complexity: 16,
		},
		{
			name:       "Unknown type 'jwt'",
			secretName: "test-secret",
			secretType: "jwt",
			complexity: 16,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret, err := cluster.NewSecret(tc.secretName, tc.secretType, tc.complexity)

			// Should return an error
			if err == nil {
				t.Error("Expected error for invalid secret type, got nil")
			}

			// Error message should mention unsupported type
			if err != nil && err.Error() != cluster.ErrUnsupportedType {
				t.Errorf("Expected error %q, got %q", cluster.ErrUnsupportedType, err.Error())
			}

			// Secret should still be returned (but with empty value)
			if secret == nil {
				t.Error("Expected secret to be returned even on error")
			}

			if secret != nil && secret.Value != "" {
				t.Error("Expected empty secret value on error")
			}
		})
	}
}

// TestNewSecretRandomness tests that random secrets are actually random.
func TestNewSecretRandomness(t *testing.T) {
	const numSecrets = 10
	const complexity = 32

	secrets := make([]string, numSecrets)

	for i := range numSecrets {
		secret, err := cluster.NewSecret("test-secret", "random", complexity)
		if err != nil {
			t.Fatalf("Failed to generate secret %d: %v", i, err)
		}
		secrets[i] = secret.Value
	}

	// Check that all secrets are unique
	for i := range numSecrets {
		for j := i + 1; j < numSecrets; j++ {
			if secrets[i] == secrets[j] {
				t.Errorf("Secrets %d and %d are identical: %q", i, j, secrets[i])
			}
		}
	}
}

// TestNewSecretEdgeCases tests edge cases for secret generation.
func TestNewSecretEdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		secretName string
		secretType string
		complexity int
		expectErr  bool
	}{
		{
			name:       "Zero complexity random secret",
			secretName: "zero-secret",
			secretType: "random",
			complexity: 0,
			expectErr:  false, // Should succeed with empty string
		},
		{
			name:       "Single character random secret",
			secretName: "tiny-secret",
			secretType: "random",
			complexity: 1,
			expectErr:  false,
		},
		{
			name:       "Very large random secret",
			secretName: "huge-secret",
			secretType: "random",
			complexity: 1024,
			expectErr:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret, err := cluster.NewSecret(tc.secretName, tc.secretType, tc.complexity)

			if tc.expectErr && err == nil {
				t.Error("Expected error but got nil")
			}

			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tc.expectErr && secret != nil {
				if len(secret.Value) != tc.complexity {
					t.Errorf("Expected value length %d, got %d", tc.complexity, len(secret.Value))
				}
			}
		})
	}
}

// TestSecretDefaultComplexity tests the default complexity constant.
func TestSecretDefaultComplexity(t *testing.T) {
	expectedComplexity := 16

	if cluster.SecretDefaultComplexity != expectedComplexity {
		t.Errorf("Expected cluster.SecretDefaultComplexity to be %d, got %d", expectedComplexity, cluster.SecretDefaultComplexity)
	}

	// Test using the constant
	secret, err := cluster.NewSecret("default-secret", "random", cluster.SecretDefaultComplexity)
	if err != nil {
		t.Fatalf("Failed to generate secret with default complexity: %v", err)
	}

	if len(secret.Value) != cluster.SecretDefaultComplexity {
		t.Errorf("Expected secret length %d, got %d", cluster.SecretDefaultComplexity, len(secret.Value))
	}
}

// BenchmarkNewSecretRandom benchmarks random secret generation.
func BenchmarkNewSecretRandom(b *testing.B) {
	for b.Loop() {
		_, err := cluster.NewSecret("test-secret", "random", 32)
		if err != nil {
			b.Fatalf("Failed to generate secret: %v", err)
		}
	}
}

// BenchmarkNewSecretOAuth benchmarks OAuth secret generation.
func BenchmarkNewSecretOAuth(b *testing.B) {
	for b.Loop() {
		_, err := cluster.NewSecret("test-secret", "oauth", 32)
		if err != nil {
			b.Fatalf("Failed to generate secret: %v", err)
		}
	}
}
