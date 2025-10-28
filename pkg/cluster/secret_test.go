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

	secret, err := cluster.NewSecret("test-secret", "random", 16)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(secret).ShouldNot(BeNil())
	g.Expect(secret.Name).Should(Equal("test-secret"))
	g.Expect(secret.Type).Should(Equal("random"))
	g.Expect(secret.Complexity).Should(Equal(16))
	g.Expect(secret.Value).Should(HaveLen(16))

	// Check that value contains only valid characters (from LetterRunes)
	for i, char := range secret.Value {
		g.Expect(strings.ContainsRune(cluster.LetterRunes, char)).Should(BeTrue(),
			"Invalid character %q at position %d in secret value", char, i)
	}
}

// TestNewSecretWithOAuthType tests the creation of an oauth-type secret.
func TestNewSecretWithOAuthType(t *testing.T) {
	g := NewWithT(t)

	secret, err := cluster.NewSecret("oauth-secret", "oauth", 16)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(secret).ShouldNot(BeNil())
	g.Expect(secret.Name).Should(Equal("oauth-secret"))
	g.Expect(secret.Type).Should(Equal("oauth"))
	g.Expect(secret.Complexity).Should(Equal(16))
	g.Expect(secret.Value).ShouldNot(BeEmpty())

	// Verify it's double base64 encoded
	firstDecode, err := base64.StdEncoding.DecodeString(secret.Value)
	g.Expect(err).ShouldNot(HaveOccurred(), "Failed to decode first layer of base64")

	secondDecode, err := base64.StdEncoding.DecodeString(string(firstDecode))
	g.Expect(err).ShouldNot(HaveOccurred(), "Failed to decode second layer of base64")

	// The final decoded value should have the original complexity length
	g.Expect(secondDecode).Should(HaveLen(16))
}

// TestNewSecretWithInvalidInputs tests error handling for invalid inputs.
func TestNewSecretWithInvalidInputs(t *testing.T) {
	testCases := []struct {
		name        string
		secretName  string
		secretType  string
		complexity  int
		expectedErr string
		shouldBeNil bool
	}{
		{
			name:        "Empty name",
			secretName:  "",
			secretType:  "random",
			complexity:  16,
			expectedErr: cluster.ErrEmptyName,
			shouldBeNil: true,
		},
		{
			name:        "Empty type",
			secretName:  "test-secret",
			secretType:  "",
			complexity:  16,
			expectedErr: cluster.ErrEmptyType,
			shouldBeNil: true,
		},
		{
			name:        "Zero complexity",
			secretName:  "test-secret",
			secretType:  "random",
			complexity:  0,
			expectedErr: cluster.ErrInvalidComplexity,
			shouldBeNil: true,
		},
		{
			name:        "Negative complexity",
			secretName:  "test-secret",
			secretType:  "random",
			complexity:  -5,
			expectedErr: cluster.ErrInvalidComplexity,
			shouldBeNil: true,
		},
		{
			name:        "Invalid type 'invalid'",
			secretName:  "test-secret",
			secretType:  "invalid",
			complexity:  16,
			expectedErr: cluster.ErrUnsupportedType,
			shouldBeNil: false,
		},
		{
			name:        "Unknown type 'jwt'",
			secretName:  "test-secret",
			secretType:  "jwt",
			complexity:  16,
			expectedErr: cluster.ErrUnsupportedType,
			shouldBeNil: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			secret, err := cluster.NewSecret(tc.secretName, tc.secretType, tc.complexity)

			// Should return an error
			g.Expect(err).Should(HaveOccurred(), "Expected error for invalid input")

			// Error message should match expected
			g.Expect(err.Error()).Should(Equal(tc.expectedErr))

			// Check if secret should be nil (validation errors) or not (generation errors)
			if tc.shouldBeNil {
				g.Expect(secret).Should(BeNil(), "Expected nil secret for validation error")
			} else {
				g.Expect(secret).ShouldNot(BeNil(), "Expected secret to be returned for generation error")
				g.Expect(secret.Value).Should(BeEmpty(), "Expected empty secret value on error")
			}
		})
	}
}
