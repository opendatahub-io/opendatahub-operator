package secret

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"strconv"
)

const (
	SecretNameAnnotation        = "secret-generator.opendatahub.io/name"
	SecretTypeAnnotation        = "secret-generator.opendatahub.io/type"
	SecretLengthAnnotation      = "secret-generator.opendatahub.io/complexity"
	SecretOauthClientAnnotation = "secret-generator.opendatahub.io/oauth-client-route"
	SecretDefaultComplexity     = 16

	letterRunes = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	ErrEmptyAnnotation        = "secret annotations is empty"
	ErrNameAnnotationNotFound = "name annotation not found in secret"
	ErrTypeAnnotationNotFound = "type annotation not found in secret"
	ErrUnsupportedType        = "secret type is not supported"
)

type Secret struct {
	Name             string
	Type             string
	Complexity       int
	Value            string
	OAuthClientRoute string
}

func NewSecretFrom(annotations map[string]string) (*Secret, error) {
	// Check if annotations is not empty
	if len(annotations) == 0 {
		return nil, errors.New(ErrEmptyAnnotation)
	}

	var secret Secret

	// Get name from annotation
	if secretName, found := annotations[SecretNameAnnotation]; found {
		secret.Name = secretName
	} else {
		return nil, errors.New(ErrNameAnnotationNotFound)
	}

	// Get type from annotation
	if secretType, found := annotations[SecretTypeAnnotation]; found {
		secret.Type = secretType
	} else {
		return nil, errors.New(ErrTypeAnnotationNotFound)
	}

	// Get complexity from annotation
	if secretComplexity, found := annotations[SecretLengthAnnotation]; found {
		secretComplexity, err := strconv.Atoi(secretComplexity)
		if err != nil {
			return nil, err
		}
		secret.Complexity = secretComplexity
	} else {
		secret.Complexity = SecretDefaultComplexity
	}

	if secretOAuthClientRoute, found := annotations[SecretOauthClientAnnotation]; found {
		secret.OAuthClientRoute = secretOAuthClientRoute
	}

	if err := generateSecretValue(&secret); err != nil {
		return nil, err
	}

	return &secret, nil
}

func NewSecret(name, secretType string, complexity int) (*Secret, error) {
	secret := &Secret{
		Name:       name,
		Type:       secretType,
		Complexity: complexity,
	}

	err := generateSecretValue(secret)

	return secret, err
}

func generateSecretValue(secret *Secret) error {
	switch secret.Type {
	case "random":
		randomValue := make([]byte, secret.Complexity)
		for i := 0; i < secret.Complexity; i++ {
			num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letterRunes))))
			if err != nil {
				return err
			}
			randomValue[i] = letterRunes[num.Int64()]
		}
		secret.Value = string(randomValue)
	case "oauth":
		randomValue := make([]byte, secret.Complexity)
		if _, err := rand.Read(randomValue); err != nil {
			return err
		}
		secret.Value = base64.StdEncoding.EncodeToString(
			[]byte(base64.StdEncoding.EncodeToString(randomValue)))
	default:
		return errors.New(ErrUnsupportedType)
	}
	return nil
}
