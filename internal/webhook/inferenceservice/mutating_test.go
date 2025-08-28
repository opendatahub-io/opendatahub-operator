package inferenceservice_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/inferenceservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	testNamespace        = "glue-ns"
	testInferenceService = "glue-isvc"
	testSecret           = "glue-secret"
	OperationRemove      = "remove"
)

type TestCase struct {
	name               string
	secretType         string
	secretData         map[string][]byte
	secretNamespace    string
	annotations        map[string]string
	predictorSpec      map[string]interface{}
	operation          admissionv1.Operation
	expectedAllowed    bool
	expectedMessage    string
	expectedPatchCheck func([]jsonpatch.JsonPatchOperation) bool
}

func setupTestEnvironment(t *testing.T) (*runtime.Scheme, context.Context) {
	t.Helper()
	sch, err := scheme.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	return sch, t.Context()
}

func createWebhook(cli client.Client, sch *runtime.Scheme) *inferenceservice.ConnectionWebhook {
	webhook := &inferenceservice.ConnectionWebhook{
		Client:  cli,
		Decoder: admission.NewDecoder(sch),
		Name:    "glueisvc-test",
	}
	return webhook
}

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

func createTestInferenceService(name, namespace string, annotations map[string]string, predictorSpec map[string]interface{}) (*unstructured.Unstructured, error) {
	isvc := envtestutil.NewInferenceService(name, namespace)
	unstructuredISVC, ok := isvc.(*unstructured.Unstructured)
	if !ok {
		return nil, errors.New("failed to cast InferenceService to unstructured")
	}

	if annotations != nil {
		unstructuredISVC.SetAnnotations(annotations)
	}

	if len(predictorSpec) > 0 {
		if err := unstructured.SetNestedMap(unstructuredISVC.Object, predictorSpec, "spec", "predictor"); err != nil {
			return nil, fmt.Errorf("failed to set nested map: %w", err)
		}
	}

	return unstructuredISVC, nil
}

func runTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	var cli client.Client
	if tc.secretType != "" {
		// Extract secret name from annotations, default to testSecret if not specified
		secretName := testSecret
		if tc.annotations != nil {
			if name, exists := tc.annotations[annotations.Connection]; exists {
				secretName = name
			}
		}

		secret := createTestSecret(secretName, tc.secretNamespace, tc.secretType, tc.secretData)
		cli = fake.NewClientBuilder().WithScheme(sch).WithObjects(secret).Build()
	} else {
		cli = fake.NewClientBuilder().WithScheme(sch).Build()
	}

	webhook := createWebhook(cli, sch)

	isvc, err := createTestInferenceService(testInferenceService, testNamespace, tc.annotations, tc.predictorSpec)
	if err != nil {
		t.Fatalf("failed to create test InferenceService: %v", err)
	}
	isvcRaw, err := json.Marshal(isvc)
	if err != nil {
		t.Fatalf("failed to marshal InferenceService: %v", err)
	}

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: tc.operation,
			Namespace: testNamespace,
			Object: runtime.RawExtension{
				Raw: isvcRaw,
			},
			Kind: metav1.GroupVersionKind{
				Group:   gvk.InferenceServices.Group,
				Version: gvk.InferenceServices.Version,
				Kind:    gvk.InferenceServices.Kind,
			},
		},
	}

	resp := webhook.Handle(ctx, req)
	g.Expect(resp.Allowed).To(Equal(tc.expectedAllowed))

	if tc.expectedMessage != "" && resp.Result != nil {
		g.Expect(resp.Result.Message).To(ContainSubstring(tc.expectedMessage))
	}

	if tc.expectedPatchCheck != nil {
		g.Expect(tc.expectedPatchCheck(resp.Patches)).To(BeTrue())
	}
}

// oci-v1 - simple case for new injection without existing secrets.
func hasImagePullSecretsPatch(expectedSecretName string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/imagePullSecrets" {
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

// uri-v1.
func hasStorageUriPatch(expectedUri string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/model/storageUri" {
				if expectedUri == "" {
					return true
				}
				return patch.Value == expectedUri
			}
		}
		return false
	}
}

// s3.
func hasStorageKeyPatch(expectedStorageKey string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/model/storage/key" {
				if expectedStorageKey == "" {
					return true
				}
				return patch.Value == expectedStorageKey
			}
			if patch.Path == "/spec/predictor/model/storage" {
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

// uri-v1.
func hasStorageUriCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/model/storageUri" && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

// s3.
func hasStorageKeyCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/model/storage" && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

func TestConnectionWebhook(t *testing.T) {
	testCases := []TestCase{
		// general cases
		{
			name:            "no connection annotation set, ISVC should be allowed to create",
			secretType:      "",
			annotations:     nil,
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "no action needed",
		},
		{
			name:            "secret exists but has no allowed connection type annotation, ISVC should be allowed to create with no injection",
			secretType:      "other-type",
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{"key": []byte("value")},
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "no action needed",
		},
		{
			name:            "to delete ISVC with allowed type should be passed",
			secretType:      inferenceservice.ConnectionTypeS3.String(),
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Delete,
			expectedAllowed: true,
			expectedMessage: "Operation DELETE",
		},
		{
			name:            "secret not found regardless not exist or in a different namespace, ISVC should not be allowed",
			secretType:      "",
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "not found",
		},
		// type cases for new creation
		{
			name:            "unsupported type set in the annoation, ISVC should be allow to create but no injection",
			secretType:      "new-type",
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "no action needed",
		},
		{
			name:               "annotation as OCI type, ISVC creation allowed with injection done",
			secretType:         inferenceservice.ConnectionTypeOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:               "annotation as URI type with model in spec, ISVC creation allowed with injection done",
			secretType:         inferenceservice.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("https://opendathub.io/model")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriPatch("https://opendathub.io/model"),
		},
		{
			name:               "annotation as S3 type, ISVC creation allowed with injection done",
			secretType:         inferenceservice.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch(testSecret),
		},
		{
			name:            "annotation as URI type without data.URI set, ISVC should not be allowed to create",
			secretType:      inferenceservice.ConnectionTypeURI.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{},
			annotations:     map[string]string{annotations.Connection: testSecret},
			predictorSpec:   map[string]interface{}{"model": map[string]interface{}{}},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "secret does not contain 'URI' data key",
		},
		// type cases for update
		{
			name:               "annotation as S3 type with existing storageUri, ISVC update allowed with replacement",
			secretType:         inferenceservice.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{},
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{"key": "existing-secret"}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch(testSecret),
		},
		{
			name:               "annotation as OCI type, ISVC update allowed with replacement",
			secretType:         inferenceservice.ConnectionTypeOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:            "annotation as S3 type without model set, ISVC should not be allowed to create",
			secretType:      inferenceservice.ConnectionTypeS3.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "found no spec.predictor.model set in resource",
		},
		{
			name:               "annotation as URI type with new URI, ISVC should overwrite with new value in the patch",
			secretType:         inferenceservice.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("s3://new-bucket/new-model")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{"URI": "s3://old-bucket/old-model"}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriPatch("s3://new-bucket/new-model"),
		},
		// Cleanup tests when annotation is removed
		{
			name:            "annotation removed, OCI filed is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			predictorSpec: map[string]interface{}{
				"imagePullSecrets": []interface{}{
					map[string]interface{}{"name": testSecret},
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsCleanupPatch(),
		},
		{
			name:            "annotation removed, URI is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			predictorSpec: map[string]interface{}{
				"model": map[string]interface{}{
					"storageUri": testSecret,
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriCleanupPatch(),
		},
		{
			name:            "annotation removed, S3 is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			predictorSpec: map[string]interface{}{
				"model": map[string]interface{}{
					"storage": map[string]interface{}{"key": testSecret},
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyCleanupPatch(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runTestCase(t, tc)
		})
	}
}
