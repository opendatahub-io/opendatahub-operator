package cluster_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// TestNewSecretWithRandomType tests the creation of a random-type secret.
func TestNewSecretWithRandomType(t *testing.T) {
	secret, err := cluster.NewSecret("test-secret", "random", cluster.SecretDefaultComplexity)
	if err != nil {
		t.Errorf("NewSecret returned unexpected error: %v", err)
	}

	if secret == nil {
		t.Errorf("NewSecret returned nil secret")
		return
	}

	if secret.Name != "test-secret" {
		t.Errorf("Expected secret name %q, got %q", "test-secret", secret.Name)
	}

	if secret.Type != "random" {
		t.Errorf("Expected secret type %q, got %q", "random", secret.Type)
	}

	if secret.Complexity != cluster.SecretDefaultComplexity {
		t.Errorf("Expected complexity %d, got %d", cluster.SecretDefaultComplexity, secret.Complexity)
	}

	if len(secret.Value) != cluster.SecretDefaultComplexity {
		t.Errorf("Expected value length %d, got %d", cluster.SecretDefaultComplexity, len(secret.Value))
	}

	// Check that value contains only valid characters (from LetterRunes)
	for i, char := range secret.Value {
		if !strings.ContainsRune(cluster.LetterRunes, char) {
			t.Errorf("Invalid character %q at position %d in secret value", char, i)
		}
	}
}

// TestNewSecretWithOAuthType tests the creation of an oauth-type secret.
func TestNewSecretWithOAuthType(t *testing.T) {
	secret, err := cluster.NewSecret("oauth-secret", "oauth", cluster.SecretDefaultComplexity)
	if err != nil {
		t.Errorf("NewSecret returned unexpected error: %v", err)
	}

	if secret == nil {
		t.Errorf("NewSecret returned nil secret")
		return
	}

	if secret.Name != "oauth-secret" {
		t.Errorf("Expected secret name %q, got %q", "oauth-secret", secret.Name)
	}

	if secret.Type != "oauth" {
		t.Errorf("Expected secret type %q, got %q", "oauth", secret.Type)
	}

	if secret.Complexity != cluster.SecretDefaultComplexity {
		t.Errorf("Expected complexity %d, got %d", cluster.SecretDefaultComplexity, secret.Complexity)
	}

	if secret.Value == "" {
		t.Errorf("Expected non-empty secret value")
	}

	// Verify it's double base64 encoded
	firstDecode, err := base64.StdEncoding.DecodeString(secret.Value)
	if err != nil {
		t.Errorf("Failed to decode first layer of base64: %v", err)
	}

	secondDecode, err := base64.StdEncoding.DecodeString(string(firstDecode))
	if err != nil {
		t.Errorf("Failed to decode second layer of base64: %v", err)
	}

	// The final decoded value should have the original complexity length
	if len(secondDecode) != cluster.SecretDefaultComplexity {
		t.Errorf("Expected decoded value length %d, got %d", cluster.SecretDefaultComplexity, len(secondDecode))
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
