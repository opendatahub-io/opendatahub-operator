package secretgenerator

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"strconv"
)

const (
	SECRET_NAME_ANNOTATION         = "secret-generator.opendatahub.io/name"
	SECRET_TYPE_ANNOTATION         = "secret-generator.opendatahub.io/type"
	SECRET_LENGTH_ANNOTATION       = "secret-generator.opendatahub.io/complexity"
	SECRET_OAUTH_CLIENT_ANNOTATION = "secret-generator.opendatahub.io/oauth-client-route"
	SECRET_DEFAULT_COMPLEXITY      = 16

	letterRunes = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	errEmptyAnnotation        = "secret annotations is empty"
	errNameAnnotationNotFound = "name annotation not found in secret"
	errTypeAnnotationNotFound = "type annotation not found in secret"
	errUnsupportedType        = "secret type is not supported"
)

type Secret struct {
	Name             string
	Type             string
	Complexity       int
	Value            string
	OAuthClientRoute string
}

func newSecret(annotations map[string]string) (*Secret, error) {
	// Check if annotations is not empty
	if len(annotations) == 0 {
		return nil, errors.New(errEmptyAnnotation)
	}

	var secret Secret

	// Get name from annotation
	if secretName, found := annotations[SECRET_NAME_ANNOTATION]; found {
		secret.Name = secretName
	} else {
		return nil, errors.New(errNameAnnotationNotFound)
	}

	// Get type from annotation
	if secretType, found := annotations[SECRET_TYPE_ANNOTATION]; found {
		secret.Type = secretType
	} else {
		return nil, errors.New(errTypeAnnotationNotFound)
	}

	// Get complexity from annotation
	if secretComplexity, found := annotations[SECRET_LENGTH_ANNOTATION]; found {
		secretComplexity, err := strconv.Atoi(secretComplexity)
		if err != nil {
			return nil, err
		}
		secret.Complexity = secretComplexity
	} else {
		secret.Complexity = SECRET_DEFAULT_COMPLEXITY
	}

	// Generate a random value based on the secret type
	switch secret.Type {
	case "random":
		randomValue := make([]byte, secret.Complexity)
		for i := 0; i < secret.Complexity; i++ {
			num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letterRunes))))
			if err != nil {
				return nil, err
			}
			randomValue[i] = letterRunes[num.Int64()]
		}
		secret.Value = string(randomValue)
	case "oauth":
		randomValue := make([]byte, secret.Complexity)
		rand.Read(randomValue)
		secret.Value = base64.StdEncoding.EncodeToString(
			[]byte(base64.StdEncoding.EncodeToString(randomValue)))
	default:
		return nil, errors.New(errUnsupportedType)
	}
	// Get OAuthClient route name from annotation
	if secretOAuthClientRoute, found := annotations[SECRET_OAUTH_CLIENT_ANNOTATION]; found {
		secret.OAuthClientRoute = secretOAuthClientRoute
	}
	return &secret, nil
}
