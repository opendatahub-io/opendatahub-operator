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
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"

	. "github.com/onsi/gomega"
)

const (
	testNamespace        = "glue-ns"
	testInferenceService = "glue-isvc"
	testSecret           = "glue-secret"
	OperationRemove      = "remove"
	serviceAccountPath   = "/spec/predictor/serviceAccountName"
	storageUriPath       = "/spec/predictor/model/storageUri"
	storagePath          = "/spec/predictor/model/storage"
	storageKeyPath       = "/spec/predictor/model/storage/key"
	imagePullSecretsPath = "/spec/predictor/imagePullSecrets" //nolint:gosec
)

type TestCase struct {
	name               string
	secretType         string
	secretData         map[string][]byte
	secretNamespace    string
	annotations        map[string]string
	predictorSpec      map[string]interface{}
	oldAnnotations     map[string]string
	oldPredictorSpec   map[string]interface{}
	oldSecretType      string // For UPDATE operations to determine old connection type
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

func createWebhook(cli client.Client, reader client.Reader, sch *runtime.Scheme) *inferenceservice.ConnectionWebhook {
	webhook := &inferenceservice.ConnectionWebhook{
		Client:    cli,
		APIReader: reader,
		Decoder:   admission.NewDecoder(sch),
		Name:      "glueisvc-test",
	}
	return webhook
}

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

func createTestInferenceService(name, namespace string, annotations map[string]string, predictorSpec map[string]interface{}) (*unstructured.Unstructured, error) { //nolint:unparam
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
	var reader client.Reader
	var objects []client.Object

	// Create current secret if needed
	if tc.secretType != "" {
		// Extract secret name from annotations, default to testSecret if not specified
		secretName := testSecret
		if tc.annotations != nil {
			if name, exists := tc.annotations[annotations.Connection]; exists {
				secretName = name
			}
		}

		secret := createTestSecret(secretName, tc.secretNamespace, tc.secretType, tc.secretData)
		objects = append(objects, secret)
	}

	// Create old secret if needed for UPDATE operations
	if tc.operation == admissionv1.Update && tc.oldAnnotations != nil {
		if oldSecretName, exists := tc.oldAnnotations[annotations.Connection]; exists {
			// Use the specified old secret type, or default to S3 if not specified
			oldSecretType := tc.oldSecretType
			if oldSecretType == "" {
				oldSecretType = webhookutils.ConnectionTypeProtocolS3.String()
			}
			oldSecret := createTestSecret(oldSecretName, tc.secretNamespace, oldSecretType, map[string][]byte{})
			objects = append(objects, oldSecret)
		}
	}

	if len(objects) > 0 {
		cli = fake.NewClientBuilder().WithScheme(sch).WithObjects(objects...).Build()
		reader = fake.NewClientBuilder().WithScheme(sch).WithObjects(objects...).Build()
	} else {
		cli = fake.NewClientBuilder().WithScheme(sch).Build()
		reader = fake.NewClientBuilder().WithScheme(sch).Build()
	}

	webhook := createWebhook(cli, reader, sch)

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

	// For UPDATE operations, set up the old object
	if tc.operation == admissionv1.Update {
		oldIsvc, err := createTestInferenceService(testInferenceService, testNamespace, tc.oldAnnotations, tc.oldPredictorSpec)
		if err != nil {
			t.Fatalf("failed to create old InferenceService: %v", err)
		}
		oldIsvcRaw, err := json.Marshal(oldIsvc)
		if err != nil {
			t.Fatalf("failed to marshal old InferenceService: %v", err)
		}
		req.OldObject = runtime.RawExtension{
			Raw: oldIsvcRaw,
		}
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

// oci - simple case for new injection without existing secrets.
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

// uri.
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

// oci.
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

// uri.
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

func TestServiceAccountNamePatching(t *testing.T) {
	t.Run("serviceAccountName is not injected on create with OCI", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName not injected on create with OCI",
			secretType:      webhookutils.ConnectionTypeProtocolOCI.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				// OCI connections should not have ServiceAccount injection
				return !hasServiceAccountNamePatch()(patches)
			},
		}
		runTestCase(t, tc)
	})
	t.Run("serviceAccountName is not injected on create with URI", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName not injected on create with URI",
			secretType:      webhookutils.ConnectionTypeProtocolURI.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{"URI": []byte("https://example.com/model")},
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				// URI connections should not have ServiceAccount injection
				return !hasServiceAccountNamePatch()(patches)
			},
		}
		runTestCase(t, tc)
	})
	t.Run("serviceAccountName is injected on create with S3", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName injected on create with S3",
			secretType:      webhookutils.ConnectionTypeProtocolS3.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{},
			annotations:     map[string]string{annotations.Connection: testSecret},
			predictorSpec:   map[string]interface{}{"model": map[string]interface{}{}}, // S3 requires model spec
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				// S3 connections should have ServiceAccount injection
				return hasServiceAccountNamePatch()(patches)
			},
		}
		runTestCase(t, tc)
	})
	t.Run("serviceAccountName is injected on update with S3", func(t *testing.T) {
		tc := TestCase{
			name:             "serviceAccountName injected on update",
			secretType:       webhookutils.ConnectionTypeProtocolS3.String(),
			secretNamespace:  testNamespace,
			annotations:      map[string]string{annotations.Connection: testSecret},
			predictorSpec:    map[string]interface{}{"model": map[string]interface{}{}},
			oldAnnotations:   map[string]string{}, // no old annotation
			oldPredictorSpec: map[string]interface{}{"model": map[string]interface{}{}},
			operation:        admissionv1.Update,
			expectedAllowed:  true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				return hasServiceAccountNamePatch()(patches)
			},
		}
		runTestCase(t, tc)
	})
	t.Run("serviceAccountName is removed on annotation removal", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName removed on annotation removal",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // annotation removed
			predictorSpec: map[string]interface{}{
				"serviceAccountName": testSecret + "-sa",
			},
			oldAnnotations: map[string]string{annotations.Connection: testSecret},
			oldPredictorSpec: map[string]interface{}{
				"serviceAccountName": testSecret + "-sa",
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				return hasServiceAccountNameRemovePatch()(patches)
			},
		}
		runTestCase(t, tc)
	})
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
			expectedMessage: "No connection injection performed for InferenceService in namespace glue-ns",
		},
		{
			name:            "secret exists but has no allowed connection type annotation, ISVC should be allowed to create with no injection",
			secretType:      "other-type",
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{"key": []byte("value")},
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "No connection injection performed for InferenceService in namespace glue-ns",
		},
		{
			name:            "to delete ISVC with allowed type should be passed",
			secretType:      webhookutils.ConnectionTypeProtocolS3.String(),
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
			expectedMessage: "No connection injection performed for InferenceService in namespace glue-ns",
		},
		{
			name:               "annotation as OCI type, ISVC creation allowed with injection done",
			secretType:         webhookutils.ConnectionTypeProtocolOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:               "annotation as URI type with model in spec, ISVC creation allowed with injection done",
			secretType:         webhookutils.ConnectionTypeProtocolURI.String(),
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
			secretType:         webhookutils.ConnectionTypeProtocolS3.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch(testSecret),
		},
		{
			name:            "annotation as URI type without data.URI set, ISVC should not be allowed to create",
			secretType:      webhookutils.ConnectionTypeProtocolURI.String(),
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
			secretType:         webhookutils.ConnectionTypeProtocolS3.String(),
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
			secretType:         webhookutils.ConnectionTypeProtocolOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:            "annotation as S3 type without model set, ISVC should not be allowed to create",
			secretType:      webhookutils.ConnectionTypeProtocolS3.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			predictorSpec:   map[string]interface{}{"name": "test-predictor"},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "found no spec.predictor.model set in resource",
		},
		{
			name:               "annotation as URI type with new URI, ISVC should overwrite with new value in the patch",
			secretType:         webhookutils.ConnectionTypeProtocolURI.String(),
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
			oldAnnotations: map[string]string{annotations.Connection: testSecret},
			oldPredictorSpec: map[string]interface{}{
				"imagePullSecrets": []interface{}{
					map[string]interface{}{"name": testSecret},
				},
			},
			oldSecretType:      webhookutils.ConnectionTypeProtocolOCI.String(),
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
			oldAnnotations: map[string]string{annotations.Connection: testSecret},
			oldPredictorSpec: map[string]interface{}{
				"model": map[string]interface{}{
					"storageUri": testSecret,
				},
			},
			oldSecretType:      webhookutils.ConnectionTypeProtocolURI.String(),
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
				"serviceAccountName": testSecret + "-sa",
				"model": map[string]interface{}{
					"storage": map[string]interface{}{"key": testSecret},
				},
			},
			oldAnnotations: map[string]string{annotations.Connection: testSecret},
			oldPredictorSpec: map[string]interface{}{
				"serviceAccountName": testSecret + "-sa",
				"model": map[string]interface{}{
					"storage": map[string]interface{}{"key": testSecret},
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasS3CleanupPatches(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runTestCase(t, tc)
		})
	}
}

// TestDeprecatedConnectionTypeRefAnnotation tests backward compatibility with deprecated ConnectionTypeRef annotations.
func TestDeprecatedConnectionTypeRefAnnotation(t *testing.T) {
	testCases := []TestCase{
		{
			name:            "deprecated ConnectionTypeRef S3 annotation should work",
			secretType:      webhookutils.ConnectionTypeRefS3.String(), //nolint:staticcheck
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			predictorSpec:   map[string]interface{}{"model": map[string]interface{}{}},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				return hasStorageKeyPatch(testSecret)(patches) && hasServiceAccountNamePatch()(patches)
			},
		},
		{
			name:               "deprecated ConnectionTypeRef URI annotation should work",
			secretType:         webhookutils.ConnectionTypeRefURI.String(), //nolint:staticcheck
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("https://example.com/model")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriPatch("https://example.com/model"),
		},
		{
			name:               "deprecated ConnectionTypeRef OCI annotation should work",
			secretType:         webhookutils.ConnectionTypeRefOCI.String(), //nolint:staticcheck
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use the special helper to create secret with ConnectionTypeRef annotation
			runTestCaseWithCustomSecret(t, tc, func(name, namespace, connectionType string, data map[string][]byte) *corev1.Secret {
				return createTestSecretWithAnnotationType(name, namespace, connectionType, annotations.ConnectionTypeRef, data)
			})
		})
	}
}

// TestConnectionTypeValidationDenial tests that operations are denied when neither annotation exists.
func TestConnectionTypeValidationDenial(t *testing.T) {
	testCases := []TestCase{
		{
			name:            "secret without connection type annotations should be denied",
			secretType:      "", // Will be handled specially in the custom secret creator
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "does not have",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create secret without any connection type annotations
			runTestCaseWithCustomSecret(t, tc, func(name, namespace, connectionType string, data map[string][]byte) *corev1.Secret {
				return &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:        name,
						Namespace:   namespace,
						Annotations: map[string]string{}, // No connection type annotations
					},
					Data: data,
				}
			})
		})
	}
}

// TestValidateInferenceServiceConnectionType tests the new ValidateInferenceServiceConnectionType function behavior.
func TestValidateInferenceServiceConnectionType(t *testing.T) {
	g := NewWithT(t)

	// Test data setup
	allowedTypes := map[string][]string{
		annotations.ConnectionTypeProtocol: {
			webhookutils.ConnectionTypeProtocolURI.String(),
			webhookutils.ConnectionTypeProtocolS3.String(),
			webhookutils.ConnectionTypeProtocolOCI.String(),
		},
		annotations.ConnectionTypeRef: {
			webhookutils.ConnectionTypeRefURI.String(), //nolint:staticcheck
			webhookutils.ConnectionTypeRefS3.String(),  //nolint:staticcheck
			webhookutils.ConnectionTypeRefOCI.String(), //nolint:staticcheck
		},
	}

	testCases := []struct {
		name               string
		protocolAnnotation string
		refAnnotation      string
		expectedType       string
		expectedValid      bool
	}{
		{
			name:               "valid ConnectionTypeProtocol annotation",
			protocolAnnotation: webhookutils.ConnectionTypeProtocolS3.String(),
			refAnnotation:      "",
			expectedType:       webhookutils.ConnectionTypeProtocolS3.String(),
			expectedValid:      true,
		},
		{
			name:               "empty protocol falls back to ref",
			protocolAnnotation: "",
			refAnnotation:      webhookutils.ConnectionTypeRefOCI.String(), //nolint:staticcheck
			expectedType:       webhookutils.ConnectionTypeRefOCI.String(), //nolint:staticcheck
			expectedValid:      true,
		},
		{
			name:               "neither annotation exists",
			protocolAnnotation: "",
			refAnnotation:      "",
			expectedType:       "",
			expectedValid:      false,
		},
		{
			name:               "invalid protocol type",
			protocolAnnotation: "invalid-type",
			refAnnotation:      "",
			expectedType:       "invalid-type",
			expectedValid:      false,
		},
		{
			name:               "invalid ref type",
			protocolAnnotation: "",
			refAnnotation:      "invalid-ref-type",
			expectedType:       "invalid-ref-type",
			expectedValid:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create secret metadata with the specified annotations
			secretMeta := &metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testSecret,
					Namespace:   testNamespace,
					Annotations: make(map[string]string),
				},
			}

			if tc.protocolAnnotation != "" {
				secretMeta.Annotations[annotations.ConnectionTypeProtocol] = tc.protocolAnnotation
			}
			if tc.refAnnotation != "" {
				secretMeta.Annotations[annotations.ConnectionTypeRef] = tc.refAnnotation
			}

			// Call the function under test
			connectionType, isValid := webhookutils.ValidateInferenceServiceConnectionType(secretMeta, allowedTypes)

			// Verify results
			g.Expect(connectionType).To(Equal(tc.expectedType), "Expected connection type: %s, got: %s", tc.expectedType, connectionType)
			g.Expect(isValid).To(Equal(tc.expectedValid), "Expected valid: %v, got: %v", tc.expectedValid, isValid)
		})
	}
}

// runTestCaseWithCustomSecret runs a test case with a custom secret creation function.
func runTestCaseWithCustomSecret(t *testing.T, tc TestCase, secretCreator func(name, namespace, connectionType string, data map[string][]byte) *corev1.Secret) {
	t.Helper()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	var cli client.Client
	var reader client.Reader
	var objects []client.Object

	// Create current secret if needed
	if tc.secretType != "" || secretCreator != nil {
		// Extract secret name from annotations, default to testSecret if not specified
		secretName := testSecret
		if tc.annotations != nil {
			if name, exists := tc.annotations[annotations.Connection]; exists {
				secretName = name
			}
		}

		var secret *corev1.Secret
		if secretCreator != nil {
			secret = secretCreator(secretName, tc.secretNamespace, tc.secretType, tc.secretData)
		} else {
			secret = createTestSecret(secretName, tc.secretNamespace, tc.secretType, tc.secretData)
		}
		objects = append(objects, secret)
	}

	// Create old secret if needed for UPDATE operations
	if tc.operation == admissionv1.Update && tc.oldAnnotations != nil {
		if oldSecretName, exists := tc.oldAnnotations[annotations.Connection]; exists {
			// Use the specified old secret type, or default to S3 if not specified
			oldSecretType := tc.oldSecretType
			if oldSecretType == "" {
				oldSecretType = webhookutils.ConnectionTypeProtocolS3.String()
			}
			oldSecret := createTestSecret(oldSecretName, tc.secretNamespace, oldSecretType, map[string][]byte{})
			objects = append(objects, oldSecret)
		}
	}

	if len(objects) > 0 {
		cli = fake.NewClientBuilder().WithScheme(sch).WithObjects(objects...).Build()
		reader = fake.NewClientBuilder().WithScheme(sch).WithObjects(objects...).Build()
	} else {
		cli = fake.NewClientBuilder().WithScheme(sch).Build()
		reader = fake.NewClientBuilder().WithScheme(sch).Build()
	}

	webhook := createWebhook(cli, reader, sch)

	// Create current InferenceService
	isvc, err := createTestInferenceService(testInferenceService, testNamespace, tc.annotations, tc.predictorSpec)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create old InferenceService for UPDATE operations
	var oldISVC *unstructured.Unstructured
	if tc.operation == admissionv1.Update {
		oldISVC, err = createTestInferenceService(testInferenceService, testNamespace, tc.oldAnnotations, tc.oldPredictorSpec)
		g.Expect(err).ShouldNot(HaveOccurred())
	}

	// Create admission request
	isvcRaw, err := json.Marshal(isvc)
	g.Expect(err).ShouldNot(HaveOccurred())

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

	// For UPDATE operations, set up the old object
	if tc.operation == admissionv1.Update && oldISVC != nil {
		oldIsvcRaw, err := json.Marshal(oldISVC)
		g.Expect(err).ShouldNot(HaveOccurred())
		req.OldObject = runtime.RawExtension{
			Raw: oldIsvcRaw,
		}
	}

	// Call the webhook
	resp := webhook.Handle(ctx, req)

	// Verify response
	g.Expect(resp.Allowed).To(Equal(tc.expectedAllowed), "Expected allowed: %v, got: %v", tc.expectedAllowed, resp.Allowed)

	if tc.expectedMessage != "" {
		g.Expect(resp.Result.Message).To(ContainSubstring(tc.expectedMessage), "Expected message to contain: %s, got: %s", tc.expectedMessage, resp.Result.Message)
	}

	if tc.expectedPatchCheck != nil {
		g.Expect(tc.expectedPatchCheck(resp.Patches)).To(BeTrue())
	}
}
