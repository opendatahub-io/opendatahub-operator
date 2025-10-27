package cluster_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

// TestNewSecretWithRandomType tests the creation of a random-type secret.
func TestNewSecretWithRandomType(t *testing.T) {
	g := NewWithT(t)

	secret, err := cluster.NewSecret("test-secret", "random", cluster.SecretDefaultComplexity)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(secret).ShouldNot(BeNil())
	g.Expect(secret.Name).Should(Equal("test-secret"))
	g.Expect(secret.Type).Should(Equal("random"))
	g.Expect(secret.Complexity).Should(Equal(cluster.SecretDefaultComplexity))
	g.Expect(secret.Value).Should(HaveLen(cluster.SecretDefaultComplexity))

	// Check that value contains only valid characters (from LetterRunes)
	for i, char := range secret.Value {
		g.Expect(strings.ContainsRune(cluster.LetterRunes, char)).Should(BeTrue(),
			"Invalid character %q at position %d in secret value", char, i)
	}
}

// TestNewSecretWithOAuthType tests the creation of an oauth-type secret.
func TestNewSecretWithOAuthType(t *testing.T) {
	g := NewWithT(t)

	secret, err := cluster.NewSecret("oauth-secret", "oauth", cluster.SecretDefaultComplexity)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(secret).ShouldNot(BeNil())
	g.Expect(secret.Name).Should(Equal("oauth-secret"))
	g.Expect(secret.Type).Should(Equal("oauth"))
	g.Expect(secret.Complexity).Should(Equal(cluster.SecretDefaultComplexity))
	g.Expect(secret.Value).ShouldNot(BeEmpty())

	// Verify it's double base64 encoded
	firstDecode, err := base64.StdEncoding.DecodeString(secret.Value)
	g.Expect(err).ShouldNot(HaveOccurred(), "Failed to decode first layer of base64")

	secondDecode, err := base64.StdEncoding.DecodeString(string(firstDecode))
	g.Expect(err).ShouldNot(HaveOccurred(), "Failed to decode second layer of base64")

	// The final decoded value should have the original complexity length
	g.Expect(secondDecode).Should(HaveLen(cluster.SecretDefaultComplexity))
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
			g := NewWithT(t)

			secret, err := cluster.NewSecret(tc.secretName, tc.secretType, tc.complexity)

			// Should return an error
			g.Expect(err).Should(HaveOccurred(), "Expected error for invalid secret type")

			// Error message should mention unsupported type
			g.Expect(err.Error()).Should(Equal(cluster.ErrUnsupportedType))

			// Secret should still be returned (but with empty value)
			g.Expect(secret).ShouldNot(BeNil(), "Expected secret to be returned even on error")
			g.Expect(secret.Value).Should(BeEmpty(), "Expected empty secret value on error")
		})
	}
}
