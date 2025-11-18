// Package cluster contains authentication and secret generation utilities for the cluster.
// This package provides functions to generate cryptographically secure random secrets
// for authentication purposes such as OAuth client secrets and cookie secrets.
//
// # Migration Notes
//
// This file contains secret generation functionality that was migrated from the
// internal/controller/services/secretgenerator package to consolidate authentication
// utilities in a single location.
//
// # Secret Types
//
// Two types of secrets are supported:
//
// 1. "random" - Alphanumeric secrets using a 62-character alphabet (0-9, A-Z, a-z)
//   - Used for: Client secrets, cookie secrets, general authentication tokens
//   - Format: Direct random character selection
//   - Example: "aB3dE9gH2jK5mN8p" (for complexity 16)
//
// 2. "oauth" - Double base64-encoded random bytes
//   - Used for: OAuth client secrets requiring additional encoding safety
//   - Format: Base64(Base64(random_bytes))
//   - Provides compatibility with various OAuth implementations
//
// # Security Considerations
//
// All secret generation uses crypto/rand for cryptographically secure randomness.
// The default complexity of 16 provides adequate security for most use cases,
// but can be adjusted based on specific security requirements.
package cluster

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
)

const (
	// LetterRunes defines the character set used for generating random secrets.
	// It includes digits (0-9), uppercase letters (A-Z), and lowercase letters (a-z),
	// providing a 62-character alphabet for random string generation.
	LetterRunes = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// ErrUnsupportedType is the error message returned when an unsupported secret type is requested.
	ErrUnsupportedType = "secret type is not supported"

	// ErrEmptyName is the error message returned when an empty secret name is provided.
	ErrEmptyName = "secret name cannot be empty"

	// ErrEmptyType is the error message returned when an empty secret type is provided.
	ErrEmptyType = "secret type cannot be empty"

	// ErrInvalidComplexity is the error message returned when complexity is less than 1.
	ErrInvalidComplexity = "secret complexity must be integer and at least 1"
)

// Secret represents a generated secret with its properties.
// This structure encapsulates all the information about a secret including
// its name, type, complexity, and the generated value.
type Secret struct {
	// Name is the identifier for the secret (e.g., "client-secret", "cookie-secret").
	Name string

	// Type specifies the secret generation algorithm to use.
	// Supported values:
	//   - "random": generates a random alphanumeric string
	//   - "oauth": generates a double base64-encoded random value suitable for OAuth
	Type string

	// Complexity defines the length of the secret to generate.
	// For "random" type, this is the number of characters in the output.
	// For "oauth" type, this is the number of random bytes before encoding.
	Complexity int

	// Value contains the generated secret value.
	// This field is populated after successful secret generation.
	Value string
}

// NewSecret creates a new secret with the specified name, type, and complexity.
// It generates a cryptographically secure random value based on the specified type.
//
// Parameters:
//   - name: An identifier for the secret (e.g., "client-secret"). Cannot be empty.
//   - secretType: The type of secret to generate ("random" or "oauth"). Cannot be empty.
//   - complexity: The length/complexity of the secret to generate. Must be at least 1.
//
// Returns:
//   - *Secret: A pointer to the generated Secret struct with the Value field populated
//   - error: An error if validation fails, the secret type is unsupported, or random generation fails
//
// Supported types:
//   - "random": Generates an alphanumeric string of the specified length using
//     characters from letterRunes (0-9, A-Z, a-z). Each character is selected
//     using cryptographically secure random number generation.
//   - "oauth": Generates random bytes and double base64-encodes them. This format
//     is suitable for OAuth client secrets and provides additional encoding safety.
//
// Example:
//
//	secret, err := NewSecret("client-secret", "random", 24)
//	if err != nil {
//	    return err
//	}
//	fmt.Println(secret.Value) // e.g., "aB3dE9gH2jK5mN8pQ1rS4tU7"
func NewSecret(name, secretType string, complexity int) (*Secret, error) {
	// Validate input parameters
	if name == "" {
		return nil, errors.New(ErrEmptyName)
	}
	if secretType == "" {
		return nil, errors.New(ErrEmptyType)
	}
	if complexity < 1 {
		return nil, errors.New(ErrInvalidComplexity)
	}

	secret := &Secret{
		Name:       name,
		Type:       secretType,
		Complexity: complexity,
	}

	err := generateSecretValue(secret)

	return secret, err
}

// generateSecretValue generates the secret value based on the secret type.
// This is an internal function that populates the Value field of the Secret struct.
//
// For "random" type:
//   - Creates a byte slice of the specified complexity
//   - Each byte is filled with a randomly selected character from letterRunes
//   - Uses crypto/rand for cryptographically secure random number generation
//   - The resulting string contains only alphanumeric characters (0-9, A-Z, a-z)
//
// For "oauth" type:
//   - Generates random bytes using crypto/rand
//   - Base64-encodes the random bytes
//   - Base64-encodes the result again (double encoding)
//   - This double encoding is commonly used for OAuth secrets to ensure
//     compatibility with various systems and protocols
//
// Returns:
//   - nil on success
//   - error if the secret type is unsupported or random generation fails
func generateSecretValue(secret *Secret) error {
	switch secret.Type {
	case "random":
		// Generate a random alphanumeric string
		randomValue := make([]byte, secret.Complexity)
		for i := range secret.Complexity {
			// Use cryptographically secure random number generation
			num, err := rand.Int(rand.Reader, big.NewInt(int64(len(LetterRunes))))
			if err != nil {
				return err
			}
			// Select a random character from LetterRunes
			randomValue[i] = LetterRunes[num.Int64()]
		}
		secret.Value = string(randomValue)

	case "oauth":
		// Generate random bytes for OAuth secret
		randomValue := make([]byte, secret.Complexity)
		if _, err := rand.Read(randomValue); err != nil {
			return err
		}
		// Double base64 encode the random bytes
		// First encoding: convert random bytes to base64 string
		// Second encoding: encode the base64 string again for additional safety
		secret.Value = base64.StdEncoding.EncodeToString(
			[]byte(base64.StdEncoding.EncodeToString(randomValue)))

	default:
		// Return error for unsupported secret types
		return errors.New(ErrUnsupportedType)
	}

	return nil
}
