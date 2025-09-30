package validation

import (
	"errors"
	"fmt"
	"regexp"
)

// rfc1123NamespaceRegex is a precompiled regex for validating namespace names.
// It ensures the namespace conforms to RFC1123 DNS label rules:
// - Must start and end with alphanumeric character.
// - Can contain alphanumeric characters and hyphens in the middle.
// - Pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$.
var rfc1123NamespaceRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ValidateNamespace validates that a namespace name conforms to RFC1123 DNS label rules
// and has a maximum length of 63 characters as required by Kubernetes.
//
// Parameters:
//   - namespace: The namespace name to validate.
//
// Returns:
//   - error: Returns an error if the namespace is invalid, nil if valid.
//
// Validation rules:
//   - Namespace cannot be empty.
//   - Namespace cannot exceed 63 characters.
//   - Namespace must conform to RFC1123 DNS label rules (lowercase alphanumeric, hyphens allowed).
func ValidateNamespace(namespace string) error {
	if namespace == "" {
		return errors.New("namespace cannot be empty")
	}

	// Check length constraint (max 63 characters)
	if len(namespace) > 63 {
		return fmt.Errorf("namespace '%s' exceeds maximum length of 63 characters (length: %d)", namespace, len(namespace))
	}

	// RFC1123 DNS label regex: must start and end with alphanumeric character,
	// can contain alphanumeric characters and hyphens in the middle.
	// Pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$.
	if !rfc1123NamespaceRegex.MatchString(namespace) {
		return fmt.Errorf("namespace '%s' must be lowercase and conform to RFC1123 DNS label rules: "+
			"a-z, 0-9, '-', start/end with alphanumeric", namespace)
	}

	return nil
}
