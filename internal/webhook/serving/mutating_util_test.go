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
				annotations.ConnectionTypeProtocol: connectionType,
			},
		},
		Data: data,
	}
	return secret
}

// createTestSecretWithAnnotationType creates a test secret with the specified annotation type.
func createTestSecretWithAnnotationType(name, namespace, connectionType, annotationType string, data map[string][]byte) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				annotationType: connectionType,
			},
		},
		Data: data,
	}
	return secret
}

// for isvc.

// oci-v1 - isvc.
func hasImagePullSecretsPatch(expectedSecretName string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == isvcImagePullSecretsPath {
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
			if patch.Path == isvcStorageUriPath {
				if expectedUri == "" {
					return true
				}
				return patch.Value == expectedUri
			}
		}
		return false
	}
}

// s3 for isvc.
func hasStorageKeyPatch(expectedStorageKey string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == isvcStorageKeyPath {
				if expectedStorageKey == "" {
					return true
				}
				return patch.Value == expectedStorageKey
			}
			if patch.Path == isvcStoragePath {
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

// s3 with connection-path on isvc.
func hasStoragePathPatch(expectedPath string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == isvcStoragePathPath {
				if expectedPath == "" {
					return true
				}
				return patch.Value == expectedPath
			}
			if patch.Path == isvcStoragePath {
				if storageMap, ok := patch.Value.(map[string]interface{}); ok {
					if path, hasPath := storageMap["path"]; hasPath {
						if expectedPath == "" {
							return true
						}
						return path == expectedPath
					}
				}
			}
		}
		return false
	}
}

// uri-v1 for isvc.
func hasStorageUriCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == isvcStorageUriPath && patch.Operation == OperationRemove {
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
			if patch.Path == isvcStoragePath && patch.Operation == OperationRemove {
				hasStorageCleanup = true
			}
			if patch.Path == isvcServiceAccountPath && patch.Operation == OperationRemove {
				hasServiceAccountCleanup = true
			}
		}
		return hasStorageCleanup && hasServiceAccountCleanup
	}
}

// serviceaccount for isvc.
func hasServiceAccountNamePatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		expectedSAName := testSecret + "-sa"
		for _, patch := range patches {
			if patch.Path == isvcServiceAccountPath && patch.Value == expectedSAName {
				return true
			}
		}
		return false
	}
}

// serviceaccount for isvc.
func hasServiceAccountNameRemovePatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == isvcServiceAccountPath && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// for oci-v1.
// hasISVCImagePullSecretsCleanupPatch checks for imagePullSecrets cleanup in ISVC.
func hasISVCImagePullSecretsCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == isvcImagePullSecretsPath && patch.Operation == OperationRemove {
				return true
			}
			if patch.Path == isvcImagePullSecretsPath && patch.Operation == OperationReplace {
				return true
			}
		}
		return false
	}
}

// for llmisvc.

// uri-v1 for llmisvc.
func hasUriPath(expectedUri string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == llmisvcModelUriPath {
				return patch.Value == expectedUri
			}

			if patch.Path == llmisvcModelPath {
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

// uri-v1 for llmisvc.
func hasLLMISVCUriCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == llmisvcModelPath && patch.Operation == OperationRemove {
				return true
			}
			if patch.Path == llmisvcModelUriPath && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// serviceaccount for llmisvc.
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

// for oci-v1 on llmisvc.
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
			if patch.Path == llmisvcTemplatePath {
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

// for oci-v1 on llmisvc.
// hasLLMISVCImagePullSecretsCleanupPatch checks for imagePullSecrets cleanup in LLMISVC.
func hasLLMISVCImagePullSecretsCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == llmisvcImagePullSecretsPath && patch.Operation == OperationRemove {
				return true
			}
			if patch.Path == llmisvcImagePullSecretsPath && patch.Operation == OperationReplace {
				return true
			}
		}
		return false
	}
}
