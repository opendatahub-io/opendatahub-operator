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
	secretName                = "my-secret"
)

var (
	base64Regex = regexp.MustCompile("^[A-Za-z0-9+/]*={0,3}$")
)

func TestNewSecret(t *testing.T) {
	tests := []struct {
		name               string
		secretName         string
		secretType         string
		complexity         int
		expectedResult     string
		expectedErrMessage string
	}{
		{
			name:           "random case",
			secretName:     secretName,
			secretType:     "random",
			complexity:     1,
			expectedResult: "success",
		},
		{
			name:           "oauth case",
			secretName:     secretName,
			secretType:     "oauth",
			complexity:     1,
			expectedResult: "success",
		},
		{
			name:               "unsupported secret type",
			secretName:         secretName,
			secretType:         "·%$@&?",
			complexity:         1,
			expectedResult:     "error",
			expectedErrMessage: errUnsupportedType,
		},
		{
			name:           "zero complexity",
			secretName:     secretName,
			secretType:     "random",
			complexity:     0,
			expectedResult: "nil",
		},
		{
			name:           "empty name",
			secretName:     "",
			secretType:     "random",
			complexity:     1,
			expectedResult: "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := secretgenerator.NewSecret(tt.secretName, tt.secretType, tt.complexity)
			switch tt.expectedResult {
			case "error":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMessage)
			case "nil":
				require.NoError(t, err)
			case "success":
				require.NoError(t, err)
				require.NotNil(t, secret)
				assert.Equal(t, tt.secretName, secret.Name)
				assert.Equal(t, tt.secretType, secret.Type)
				assert.Equal(t, tt.complexity, secret.Complexity)
				assert.NotEmpty(t, secret.Value)
				assert.True(t, base64Regex.MatchString(secret.Value), "Secret value should be base64 encoded")
			default:
				assert.Empty(t, secret.Value)
				t.Fatalf("Unexpected expectedResult value: %s on the %s test", tt.expectedResult, tt.name)
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
