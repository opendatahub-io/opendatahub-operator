package serving_test

import (
	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

func createTestSecret(name, namespace, connectionType string, data map[string][]byte) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				annotations.ConnectionTypeRef: connectionType,
			},
		},
		Data: data,
	}
	return secret
}

// oci-v1 - simple case for new injection without existing secrets.
func hasImagePullSecretsPatch(expectedSecretName string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == imagePullSecretsPath {
				// Should be a single secret in the array
				if secretsList, ok := patch.Value.([]interface{}); ok && len(secretsList) == 1 {
					if secretMap, ok := secretsList[0].(map[string]interface{}); ok {
						if name, exists := secretMap["name"]; exists && name == expectedSecretName {
							return true
						}
					}
				}
			}
		}
		return false
	}
}

// uri-v1 for isvc.
func hasStorageUriPatch(expectedUri string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == storageUriPath {
				if expectedUri == "" {
					return true
				}
				return patch.Value == expectedUri
			}
		}
		return false
	}
}

// uri-v1 for llvisvc.
func hasUriPath(expectedUri string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			// Check for direct uri path patch
			if patch.Path == llmisvcModelUriPath {
				return patch.Value == expectedUri
			}

			// Check for model object patch (when model section doesn't exist initially)
			if patch.Path == "/spec/model" {
				if modelMap, ok := patch.Value.(map[string]interface{}); ok {
					if uri, exists := modelMap["uri"]; exists && uri == expectedUri {
						return true
					}
				}
			}
		}
		return false
	}
}

// s3.
func hasStorageKeyPatch(expectedStorageKey string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == storageKeyPath {
				if expectedStorageKey == "" {
					return true
				}
				return patch.Value == expectedStorageKey
			}
			if patch.Path == storagePath {
				if storageMap, ok := patch.Value.(map[string]interface{}); ok {
					if key, hasKey := storageMap["key"]; hasKey {
						if expectedStorageKey == "" {
							return true
						}
						return key == expectedStorageKey
					}
				}
			}
		}
		return false
	}
}

// oci-v1.
func hasImagePullSecretsCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/imagePullSecrets/0" && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// uri-v1 for isvc.
func hasStorageUriCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == storageUriPath && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// uri-v1 for llmisvc.
func hasLLMISVCUriCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == llmisvcModelUriPath && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// s3.
func hasS3CleanupPatches() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		hasStorageCleanup := false
		hasServiceAccountCleanup := false

		for _, patch := range patches {
			if patch.Path == storagePath && patch.Operation == OperationRemove {
				hasStorageCleanup = true
			}
			if patch.Path == serviceAccountPath && patch.Operation == OperationRemove {
				hasServiceAccountCleanup = true
			}
		}
		return hasStorageCleanup && hasServiceAccountCleanup
	}
}

func hasServiceAccountNamePatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		expectedSAName := testSecret + "-sa"
		for _, patch := range patches {
			if patch.Path == serviceAccountPath && patch.Value == expectedSAName {
				return true
			}
		}
		return false
	}
}

func hasServiceAccountNameRemovePatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == serviceAccountPath && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// hasLLMISVCServiceAccountCleanupPatch checks for service account cleanup in LLMISVC.
func hasLLMISVCServiceAccountCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == llmisvcServiceAccountPath && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// hasLLMISVCImagePullSecretsPatch checks for imagePullSecrets injection in LLMISVC.
func hasLLMISVCImagePullSecretsPatch(expectedSecretName string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			// Check for direct imagePullSecrets path
			if patch.Path == llmisvcImagePullSecretsPath {
				if secretsList, ok := patch.Value.([]interface{}); ok && len(secretsList) == 1 {
					if secretMap, ok := secretsList[0].(map[string]interface{}); ok {
						if name, exists := secretMap["name"]; exists && name == expectedSecretName {
							return true
						}
					}
				}
			}

			// Check for template object containing imagePullSecrets
			if patch.Path == "/spec/template" {
				if templateMap, ok := patch.Value.(map[string]interface{}); ok {
					if imagePullSecretsVal, exists := templateMap["imagePullSecrets"]; exists {
						if secretsList, ok := imagePullSecretsVal.([]interface{}); ok && len(secretsList) == 1 {
							if secretMap, ok := secretsList[0].(map[string]interface{}); ok {
								if name, exists := secretMap["name"]; exists && name == expectedSecretName {
									return true
								}
							}
						}
					}
				}
			}
		}
		return false
	}
}
