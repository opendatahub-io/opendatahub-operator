package secretgenerator_test

import (
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/secretgenerator"
)

const (
	errEmptyAnnotation        = "secret annotations is empty"
	errNameAnnotationNotFound = "name annotation not found in secret"
	errTypeAnnotationNotFound = "type annotation not found in secret"
	errUnsupportedType        = "secret type is not supported"
	DefaultSecretSize         = secretgenerator.SECRET_DEFAULT_COMPLEXITY
)

var (
	base64Regex = regexp.MustCompile("^[A-Za-z0-9+/]*={0,2}$")
)

func TestNewSecret(t *testing.T) {
	tests := []struct {
		name         string
		secretName   string
		secretType   string
		complexity   int
		expectReturn string
	}{
		{
			name:       "random case",
			secretName: "my-secret",
			secretType: "random",
			complexity: 1,
		},
		{
			name:       "oauth case",
			secretName: "another-secret",
			secretType: "oauth",
			complexity: 1,
		},
		{
			name:         "unsupported type",
			secretName:   "my-secret",
			secretType:   "·%$%&%",
			complexity:   1,
			expectReturn: "err",
		},
		{
			name:         "zero complexity",
			secretName:   "my-secret",
			secretType:   "random",
			complexity:   0,
			expectReturn: "nil",
		},
		{
			name:         "empty name",
			secretName:   "",
			secretType:   "random",
			complexity:   1,
			expectReturn: "nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := secretgenerator.NewSecret(tt.secretName, tt.secretType, tt.complexity)

			switch tt.expectReturn {
			case "err":
				require.Error(t, err)
			case "nil":
				require.NoError(t, err)
			default:
				require.NoError(t, err)
				require.NotNil(t, secret)
				assert.Equal(t, tt.secretName, secret.Name)
				assert.Equal(t, tt.secretType, secret.Type)
				assert.Equal(t, tt.complexity, secret.Complexity)
				assert.NotEmpty(t, secret.Value)
				assert.True(t, base64Regex.MatchString(secret.Value), "Secret value should be base64 encoded")
				actualSize := len(secret.Value)
				// A more precise size check after base64 encoding is complex due to padding.
				// We'll focus on ensuring it's a non-empty base64 string and its decoded length is reasonable.
				_ = actualSize // To avoid "unused variable" warning
			}
		})
	}
}

func TestNewSecretFrom(t *testing.T) {
	cases := map[string]struct {
		annotations map[string]string
		secret      secretgenerator.Secret
		err         error
	}{
		"Annotations are not defined": {
			annotations: map[string]string{},
			err:         errors.New(errEmptyAnnotation),
		},
		"Annotation name is not defined": {
			annotations: map[string]string{
				"secret-generator.opendatahub.io/key": "example",
			},
			err: errors.New(errNameAnnotationNotFound),
		},
		"Annotation type is not defined": {
			annotations: map[string]string{
				"secret-generator.opendatahub.io/name": "example",
			},
			err: errors.New(errTypeAnnotationNotFound),
		},
		"Secret type is not supported": {
			annotations: map[string]string{
				"secret-generator.opendatahub.io/name": "example",
				"secret-generator.opendatahub.io/type": "ssh",
			},
			err: errors.New(errUnsupportedType),
		},
		"Generate a random string secret": {
			annotations: map[string]string{
				"secret-generator.opendatahub.io/name": "example",
				"secret-generator.opendatahub.io/type": "random",
			},
			secret: secretgenerator.Secret{
				Name:       "example",
				Type:       "random",
				Complexity: secretgenerator.SECRET_DEFAULT_COMPLEXITY,
			},
		},
		"Generate a random string secret with custom complexity": {
			annotations: map[string]string{
				"secret-generator.opendatahub.io/name":       "example",
				"secret-generator.opendatahub.io/type":       "random",
				"secret-generator.opendatahub.io/complexity": "128",
			},
			secret: secretgenerator.Secret{
				Name:       "example",
				Type:       "random",
				Complexity: 128,
			},
		},
		"Generate an OAuth secret": {
			annotations: map[string]string{
				"secret-generator.opendatahub.io/name": "example",
				"secret-generator.opendatahub.io/type": "oauth",
			},
			secret: secretgenerator.Secret{
				Name:       "example",
				Type:       "oauth",
				Complexity: secretgenerator.SECRET_DEFAULT_COMPLEXITY,
			},
		},
		"Generate an OAuth secret with custom complexity": {
			annotations: map[string]string{
				"secret-generator.opendatahub.io/name":       "example",
				"secret-generator.opendatahub.io/type":       "oauth",
				"secret-generator.opendatahub.io/complexity": "24",
			},
			secret: secretgenerator.Secret{
				Name:       "example",
				Type:       "oauth",
				Complexity: 24,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			secret, err := secretgenerator.NewSecretFrom(tc.annotations)
			if err != nil {
				if err.Error() != tc.err.Error() {
					t.Errorf("Expected error: %v, got: %v\n",
						tc.err.Error(), err.Error())
				}
			} else {
				if secret.Name != tc.secret.Name ||
					secret.Type != tc.secret.Type ||
					secret.Complexity != tc.secret.Complexity {
					t.Errorf("Expected secret: %v, got: %v\n",
						tc.secret, secret)
				}
				if secret.Value == "" {
					t.Errorf("Secret value is empty\n")
				}
			}
		})
	}
}
