package serving_test

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
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/serving"
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
	spec               map[string]interface{}
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

func createWebhook(cli client.Client, reader client.Reader, sch *runtime.Scheme) *serving.ISVCConnectionWebhook {
	webhook := &serving.ISVCConnectionWebhook{
		Client:    cli,
		APIReader: reader,
		Decoder:   admission.NewDecoder(sch),
		Name:      "glueisvc-test",
	}
	return webhook
}

func createLLMISVCWebhook(cli client.Client, apireader client.Reader, sch *runtime.Scheme) *serving.LLMISVCConnectionWebhook {
	webhook := &serving.LLMISVCConnectionWebhook{
		Client:    cli,
		APIReader: apireader,
		Decoder:   admission.NewDecoder(sch),
		Name:      "gluellmisvc-test",
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

func createTestInferenceService(name, namespace string, annotations map[string]string, spec map[string]interface{}) (*unstructured.Unstructured, error) {
	isvc := envtestutil.NewInferenceService(name, namespace)
	unstructuredISVC, ok := isvc.(*unstructured.Unstructured)
	if !ok {
		return nil, errors.New("failed to cast InferenceService to unstructured")
	}

	if annotations != nil {
		unstructuredISVC.SetAnnotations(annotations)
	}

	if len(spec) > 0 {
		if err := unstructured.SetNestedMap(unstructuredISVC.Object, spec, "spec", "predictor"); err != nil {
			return nil, fmt.Errorf("failed to set nested map: %w", err)
		}
	}

	return unstructuredISVC, nil
}

func createTestLLMInferenceService(name, namespace string, annotations map[string]string, spec map[string]interface{}) (*unstructured.Unstructured, error) {
	llmISVC := envtestutil.NewLLMInferenceService(name, namespace)
	unstructuredLLMISVC, ok := llmISVC.(*unstructured.Unstructured)
	if !ok {
		return nil, errors.New("failed to cast LLMInferenceService to unstructured")
	}

	if annotations != nil {
		unstructuredLLMISVC.SetAnnotations(annotations)
	}

	if len(spec) > 0 {
		if err := unstructured.SetNestedMap(unstructuredLLMISVC.Object, spec, "spec"); err != nil {
			return nil, fmt.Errorf("failed to set nested map: %w", err)
		}
	}

	return unstructuredLLMISVC, nil
}

func runISVCTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	var cli client.Client
	var reader client.Reader
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
		reader = fake.NewClientBuilder().WithScheme(sch).WithObjects(secret).Build()
	} else {
		cli = fake.NewClientBuilder().WithScheme(sch).Build()
		reader = fake.NewClientBuilder().WithScheme(sch).Build()
	}

	webhook := createWebhook(cli, reader, sch)

	isvc, err := createTestInferenceService(testInferenceService, testNamespace, tc.annotations, tc.spec)
	if err != nil {
		t.Fatalf("failed to create test InferenceService: %v", err)
	}
	isvcRaw, err := json.Marshal(isvc)
	if err != nil {
		t.Fatalf("failed to marshal test InferenceService: %v", err)
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

func runLLMISVCTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	var cli client.Client
	var reader client.Reader
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
		reader = fake.NewClientBuilder().WithScheme(sch).WithObjects(secret).Build()
	} else {
		cli = fake.NewClientBuilder().WithScheme(sch).Build()
		reader = fake.NewClientBuilder().WithScheme(sch).WithObjects().Build()
	}

	webhook := createLLMISVCWebhook(cli, reader, sch)

	llmisvc, err := createTestLLMInferenceService(testInferenceService, testNamespace, tc.annotations, tc.spec)
	if err != nil {
		t.Fatalf("failed to create test LLMInferenceService: %v", err)
	}
	llmisvcRaw, err := json.Marshal(llmisvc)
	if err != nil {
		t.Fatalf("failed to marshal test LLMInferenceService: %v", err)
	}

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: tc.operation,
			Namespace: testNamespace,
			Object: runtime.RawExtension{
				Raw: llmisvcRaw,
			},
			Kind: metav1.GroupVersionKind{
				Group:   gvk.LLMInferenceServiceV1Alpha1.Group,
				Version: gvk.LLMInferenceServiceV1Alpha1.Version,
				Kind:    gvk.LLMInferenceServiceV1Alpha1.Kind,
			},
		},
	}

	resp := webhook.Handle(ctx, req)
	g.Expect(resp.Allowed).To(Equal(tc.expectedAllowed))

	if tc.expectedMessage != "" {
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

// uri-v1 for isvc.
func hasStorageUriPatch(expectedUri string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/model/storageUri" {
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
			if patch.Path == "/spec/model/uri" {
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

// uri-v1 for isvc.
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

// uri-v1 for llmisvc.
func hasLLMISVCUriCleanupPatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/model/uri" && patch.Operation == OperationRemove {
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

func hasServiceAccountNamePatch(expectedSAName string) func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/serviceAccountName" && patch.Value == expectedSAName {
				return true
			}
		}
		return false
	}
}

func hasServiceAccountNameRemovePatch() func([]jsonpatch.JsonPatchOperation) bool {
	return func(patches []jsonpatch.JsonPatchOperation) bool {
		for _, patch := range patches {
			if patch.Path == "/spec/predictor/serviceAccountName" && patch.Operation == OperationRemove {
				return true
			}
		}
		return false
	}
}

func TestServiceAccountNamePatching(t *testing.T) {
	t.Run("serviceAccountName is injected on create with OCI", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName injected on create",
			secretType:      serving.ConnectionTypeOCI.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				return hasServiceAccountNamePatch(testSecret + "-sa")(patches)
			},
		}
		runISVCTestCase(t, tc)
	})
	t.Run("serviceAccountName is injected on update with S3", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName injected on update",
			secretType:      serving.ConnectionTypeS3.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			spec:            map[string]interface{}{"model": map[string]interface{}{}},
			operation:       admissionv1.Update,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				return hasServiceAccountNamePatch(testSecret + "-sa")(patches)
			},
		}
		runISVCTestCase(t, tc)
	})
	t.Run("serviceAccountName is removed on annotation removal", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName removed on annotation removal",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // annotation removed
			spec: map[string]interface{}{
				"serviceAccountName": testSecret + "-sa",
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				return hasServiceAccountNameRemovePatch()(patches)
			},
		}
		runISVCTestCase(t, tc)
	})
}

func TestISVCConnectionWebhook(t *testing.T) {
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
			secretType:      serving.ConnectionTypeS3.String(),
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
			expectedMessage: "not found in namespace",
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
			secretType:         serving.ConnectionTypeOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:               "annotation as URI type with model in spec, ISVC creation allowed with injection done",
			secretType:         serving.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("https://opendathub.io/model")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			spec:               map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriPatch("https://opendathub.io/model"),
		},
		{
			name:               "annotation as S3 type, ISVC creation allowed with injection done",
			secretType:         serving.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			spec:               map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch(testSecret),
		},
		{
			name:            "annotation as URI type without data.URI/.data.https-host set in secret, ISVC should not be allowed to create",
			secretType:      serving.ConnectionTypeURI.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{},
			annotations:     map[string]string{annotations.Connection: testSecret},
			spec:            map[string]interface{}{"model": map[string]interface{}{}},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "secret does not contain either 'https-host' or 'URI' data key",
		},
		// type cases for update
		{
			name:               "annotation as S3 type with existing storageUri, ISVC update allowed with replacement",
			secretType:         serving.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{},
			annotations:        map[string]string{annotations.Connection: testSecret},
			spec:               map[string]interface{}{"model": map[string]interface{}{"key": "existing-secret"}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch(testSecret),
		},
		{
			name:               "annotation as OCI type, ISVC update allowed with replacement",
			secretType:         serving.ConnectionTypeOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			spec:               map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:            "annotation as S3 type without model set, ISVC should not be allowed to create",
			secretType:      serving.ConnectionTypeS3.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "found no spec.predictor.model set in resource",
		},
		{
			name:               "annotation as URI type with new URI, ISVC should overwrite with new value in the patch",
			secretType:         serving.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("s3://new-bucket/new-model")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			spec:               map[string]interface{}{"model": map[string]interface{}{"URI": "s3://old-bucket/old-model"}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriPatch("s3://new-bucket/new-model"),
		},
		// Cleanup tests when annotation is removed
		{
			name:            "annotation removed, imagePullSecrets is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			spec: map[string]interface{}{
				"imagePullSecrets": []interface{}{
					map[string]interface{}{"name": testSecret},
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsCleanupPatch(),
		},
		{
			name:            "annotation removed, storageUri is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			spec: map[string]interface{}{
				"model": map[string]interface{}{
					"storageUri": testSecret,
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriCleanupPatch(),
		},
		{
			name:            "annotation removed, storage is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			spec: map[string]interface{}{
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
			runISVCTestCase(t, tc)
		})
	}
}

func TestLLMISVCConnectionWebhook(t *testing.T) {
	testCases := []TestCase{
		// general cases
		{
			name:            "no connection annotation set, LLMISVC should be allowed to create",
			secretType:      "",
			annotations:     nil,
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "no action needed",
		},
		{
			name:            "secret not found regardless not exist or in a different namespace, LLMISVC should not be allowed",
			secretType:      "",
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "not found in namespace",
		},
		// type cases for create
		{
			name:               "annotation as URI type with model in spec, LLMISVC creation allowed with injection done",
			secretType:         serving.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"https-host": []byte("hf://facebook/opt-125m")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			spec:               map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasUriPath("hf://facebook/opt-125m"),
		},
		{
			name:            "secret exists but has no allowed connection type annotation, LLMISVC should be allowed to create with no injection",
			secretType:      serving.ConnectionTypeS3.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{"key": []byte("value")},
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "no action needed",
		},
		{
			name:            "annotation as URI type without data.URI/.data.https-host set in secret, LLMISVC should not be allowed to create",
			secretType:      serving.ConnectionTypeURI.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{},
			annotations:     map[string]string{annotations.Connection: testSecret},
			spec:            map[string]interface{}{"model": map[string]interface{}{}},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "secret does not contain either in 'https-host' or 'URI' data key",
		},
		{
			name:               "annotation as URI type with new host value, LLMISVC should overwrite with new value in the patch",
			secretType:         serving.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("hf://facebook/new")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			spec:               map[string]interface{}{"model": map[string]interface{}{"URI": "hf://facebook/old"}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasUriPath("hf://facebook/new"),
		},
		// cleanup tests
		{
			name:            "annotation removed, uri is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			spec: map[string]interface{}{
				"model": map[string]interface{}{
					"uri": testSecret,
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasLLMISVCUriCleanupPatch(),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runLLMISVCTestCase(t, tc)
		})
	}
}
