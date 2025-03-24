package secretgenerator_test

import (
	"errors"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/secretgenerator"
)

const (
	errEmptyAnnotation        = "secret annotations is empty"
	errNameAnnotationNotFound = "name annotation not found in secret"
	errTypeAnnotationNotFound = "type annotation not found in secret"
	errUnsupportedType        = "secret type is not supported"
)

func TestNewSecret(t *testing.T) {
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
