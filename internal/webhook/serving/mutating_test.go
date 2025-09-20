package serving_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
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
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"

	. "github.com/onsi/gomega"
)

const (
	testNamespace        = "glue-ns"
	testInferenceService = "glue-isvc"
	testSecret           = "glue-secret"

	OperationAdd     = "add"
	OperationRemove  = "remove"
	OperationReplace = "replace"
)

const (
	isvcImagePullSecretsPath      = "/spec/predictor/imagePullSecrets"   //nolint:gosec
	isvcImagePullSecretsIndexPath = "/spec/predictor/imagePullSecrets/0" //nolint:gosec
	isvcServiceAccountPath        = "/spec/predictor/serviceAccountName"
	isvcStorageUriPath            = "/spec/predictor/model/storageUri"
	isvcStoragePath               = "/spec/predictor/model/storage"
	isvcStorageKeyPath            = "/spec/predictor/model/storage/key"

	llmisvcModelPath            = "/spec/model"
	llmisvcModelUriPath         = "/spec/model/uri"
	llmisvcServiceAccountPath   = "/spec/template/serviceAccountName"
	llmisvcImagePullSecretsPath = "/spec/template/imagePullSecrets" //nolint:gosec
	llmisvcTemplatePath         = "/spec/template"
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

func createISVCWebhook(cli client.Client, reader client.Reader, sch *runtime.Scheme) *serving.ISVCConnectionWebhook {
	webhook := &serving.ISVCConnectionWebhook{
		Webhook: webhookutils.BaseServingConnectionWebhook{
			Client:    cli,
			APIReader: reader,
			Decoder:   admission.NewDecoder(sch),
			Name:      "glueisvc-test",
		},
	}
	return webhook
}

func createLLMISVCWebhook(cli client.Client, reader client.Reader, sch *runtime.Scheme) *serving.LLMISVCConnectionWebhook {
	webhook := &serving.LLMISVCConnectionWebhook{
		Webhook: webhookutils.BaseServingConnectionWebhook{
			Client:    cli,
			APIReader: reader,
			Decoder:   admission.NewDecoder(sch),
			Name:      "gluellmisvc-test",
		},
	}
	return webhook
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
				oldSecretType = webhookutils.ConnectionTypeS3.String()
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

	webhook := createISVCWebhook(cli, reader, sch)

	isvc, err := createTestInferenceService(testInferenceService, testNamespace, tc.annotations, tc.predictorSpec)
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

func runLLMISVCTestCase(t *testing.T, tc TestCase) {
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
			// Use the specified old secret type, or default to URI if not specified
			oldSecretType := tc.oldSecretType
			if oldSecretType == "" {
				oldSecretType = webhookutils.ConnectionTypeURI.String()
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

	webhook := createLLMISVCWebhook(cli, reader, sch)

	llmisvc, err := createTestLLMInferenceService(testInferenceService, testNamespace, tc.annotations, tc.predictorSpec)
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

	// For UPDATE operations, set up the old object
	if tc.operation == admissionv1.Update {
		oldLLMISVC, err := createTestLLMInferenceService(testInferenceService, testNamespace, tc.oldAnnotations, tc.oldPredictorSpec)
		if err != nil {
			t.Fatalf("failed to create old LLMInferenceService: %v", err)
		}
		oldLLMISVCRaw, err := json.Marshal(oldLLMISVC)
		if err != nil {
			t.Fatalf("failed to marshal old LLMInferenceService: %v", err)
		}
		req.OldObject = runtime.RawExtension{
			Raw: oldLLMISVCRaw,
		}
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

func TestServiceAccountNamePatching(t *testing.T) {
	t.Run("serviceAccountName is not injected on create with OCI", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName not injected on create with OCI",
			secretType:      webhookutils.ConnectionTypeOCI.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedPatchCheck: func(patches []jsonpatch.JsonPatchOperation) bool {
				// OCI connections should not have ServiceAccount injection
				return !hasServiceAccountNamePatch()(patches)
			},
		}
		runISVCTestCase(t, tc)
	})
	t.Run("serviceAccountName is not injected on create with URI", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName not injected on create with URI",
			secretType:      webhookutils.ConnectionTypeURI.String(),
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
		runISVCTestCase(t, tc)
	})
	t.Run("serviceAccountName is injected on create with S3", func(t *testing.T) {
		tc := TestCase{
			name:            "serviceAccountName injected on create with S3",
			secretType:      webhookutils.ConnectionTypeS3.String(),
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
		runISVCTestCase(t, tc)
	})
	t.Run("serviceAccountName is injected on update with S3", func(t *testing.T) {
		tc := TestCase{
			name:             "serviceAccountName injected on update",
			secretType:       webhookutils.ConnectionTypeS3.String(),
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
		runISVCTestCase(t, tc)
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
			secretType:      webhookutils.ConnectionTypeS3.String(),
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
			expectedMessage: "No connection injection performed for InferenceService in namespace glue-ns",
		},
		{
			name:               "annotation as OCI type, ISVC creation allowed with injection done",
			secretType:         webhookutils.ConnectionTypeOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:               "annotation as URI type with model in spec, ISVC creation allowed with injection done",
			secretType:         webhookutils.ConnectionTypeURI.String(),
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
			secretType:         webhookutils.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch(testSecret),
		},
		{
			name:            "annotation as URI type without data.URI/.data.https-host set in secret, ISVC should not be allowed to create",
			secretType:      webhookutils.ConnectionTypeURI.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{},
			annotations:     map[string]string{annotations.Connection: testSecret},
			predictorSpec:   map[string]interface{}{"model": map[string]interface{}{}},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "failed to inject host to .spec.predictor.model.storageUri",
		},
		// type cases for update
		{
			name:               "annotation as S3 type with existing storageUri, ISVC update allowed with replacement",
			secretType:         webhookutils.ConnectionTypeS3.String(),
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
			secretType:         webhookutils.ConnectionTypeOCI.String(),
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch(testSecret),
		},
		{
			name:            "annotation as S3 type without model set, ISVC should not be allowed to create",
			secretType:      webhookutils.ConnectionTypeS3.String(),
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			predictorSpec:   map[string]interface{}{"name": "test-predictor"},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "not found .spec.predictor.model in resource",
		},
		{
			name:               "annotation as URI type with new URI, ISVC should overwrite with new value in the patch",
			secretType:         webhookutils.ConnectionTypeURI.String(),
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
			name:            "annotation removed, imagePullSecrets is cleanup",
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
			oldSecretType:      webhookutils.ConnectionTypeOCI.String(),
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasISVCImagePullSecretsCleanupPatch(),
		},
		{
			name:            "annotation removed, storageUri is cleanup",
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
			oldSecretType:      webhookutils.ConnectionTypeURI.String(),
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriCleanupPatch(),
		},
		{
			name:            "annotation removed, storage is cleanup",
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
			expectedMessage: "No connection injection performed for LLMInferenceService in namespace glue-ns",
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
			name:               "annotation as OCI type without model section, LLMISVC creation allowed with injection done",
			secretType:         webhookutils.ConnectionTypeOCI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{},
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{}, // No model section at all
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasLLMISVCImagePullSecretsPatch(testSecret),
		},
		{
			name:            "secret exists but has no allowed connection type annotation, LLMISVC should be allowed to create with no injection",
			secretType:      "other-type",
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{"key": []byte("value")},
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "No connection injection performed for LLMInferenceService in namespace glue-ns",
		},
		{
			name:            "annotation as URI type without .data.URI/.data.https-host set in secret, LLMISVC should not be allowed to create",
			secretType:      webhookutils.ConnectionTypeURI.String(),
			secretNamespace: testNamespace,
			secretData:      map[string][]byte{},
			annotations:     map[string]string{annotations.Connection: testSecret},
			predictorSpec:   map[string]interface{}{"model": map[string]interface{}{}},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "secret does not contain either 'https-host' or 'URI' data key",
		},
		{
			name:               "annotation as S3 type, LLMISVC creation allowed with S3 URI injection",
			secretType:         webhookutils.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"AWS_S3_BUCKET": []byte("my-bucket")},
			annotations:        map[string]string{annotations.Connection: testSecret, annotations.ConnectionPath: "models/llama-7b"},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{}},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasUriPath("s3://my-bucket/models/llama-7b"),
		},
		{
			name:               "annotation as S3 type without model section, LLMISVC creation allowed with model creation and S3 URI injection",
			secretType:         webhookutils.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"AWS_S3_BUCKET": []byte("my-bucket")},
			annotations:        map[string]string{annotations.Connection: testSecret, annotations.ConnectionPath: "models/llama-7b"},
			predictorSpec:      map[string]interface{}{}, // No model section at all
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasUriPath("s3://my-bucket/models/llama-7b"),
		},
		{
			name:               "annotation as URI type without model section, LLMISVC creation allowed with model creation and URI injection",
			secretType:         webhookutils.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("hf://facebook/model")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{}, // No model section at all
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasUriPath("hf://facebook/model"),
		},
		// type cases for update
		{
			name:               "annotation as URI type with new host value, LLMISVC should overwrite with new value in the patch",
			secretType:         webhookutils.ConnectionTypeURI.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("hf://facebook/new")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{"URI": "https://facebook/old"}},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasUriPath("hf://facebook/new"),
		},
		{
			name:               "annotation as S3 type with connection-path changed, LLMISVC should update URI with new path",
			secretType:         webhookutils.ConnectionTypeS3.String(),
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"AWS_S3_BUCKET": []byte("my-bucket")},
			annotations:        map[string]string{annotations.Connection: "new-s3-secret", annotations.ConnectionPath: "models/new-path"},
			predictorSpec:      map[string]interface{}{"model": map[string]interface{}{"uri": "s3://my-bucket/models/old-path"}},
			oldAnnotations:     map[string]string{annotations.Connection: "old-s3-secret", annotations.ConnectionPath: "models/old-path"},
			oldPredictorSpec:   map[string]interface{}{"model": map[string]interface{}{"uri": "s3://my-bucket/models/old-path"}},
			oldSecretType:      webhookutils.ConnectionTypeS3.String(),
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasUriPath("s3://my-bucket/models/new-path"),
		},
		// cleanup tests
		{
			name:            "annotation removed, uri is cleanup for any type of connection previously injected",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			predictorSpec: map[string]interface{}{
				"model": map[string]interface{}{
					"uri": testSecret,
				},
			},
			oldAnnotations: map[string]string{annotations.Connection: testSecret},
			oldPredictorSpec: map[string]interface{}{
				"model": map[string]interface{}{
					"uri": testSecret,
				},
			},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasLLMISVCUriCleanupPatch(),
		},
		{
			name:            "annotation removed for S3 type, serviceAccountName is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			predictorSpec: map[string]interface{}{
				"template": map[string]interface{}{
					"serviceAccountName": testSecret + "-sa",
				},
				"model": map[string]interface{}{
					"uri": "s3://my-ml-models/models/llama-7b",
				},
			},
			oldAnnotations: map[string]string{annotations.Connection: testSecret},
			oldPredictorSpec: map[string]interface{}{
				"template": map[string]interface{}{
					"serviceAccountName": testSecret + "-sa",
				},
				"model": map[string]interface{}{
					"uri": "s3://my-ml-models/models/llama-7b",
				},
			},
			oldSecretType:      webhookutils.ConnectionTypeS3.String(),
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasLLMISVCServiceAccountCleanupPatch(),
		},
		{
			name:            "annotation removed for OCI type, imagePullSecrets is cleanup",
			secretType:      "",
			secretNamespace: testNamespace,
			annotations:     map[string]string{}, // no annotation
			predictorSpec: map[string]interface{}{
				"template": map[string]interface{}{
					"imagePullSecrets": []interface{}{
						map[string]interface{}{
							"name": testSecret,
						},
					},
				},
			},
			oldAnnotations: map[string]string{annotations.Connection: testSecret},
			oldPredictorSpec: map[string]interface{}{
				"template": map[string]interface{}{
					"imagePullSecrets": []interface{}{
						map[string]interface{}{
							"name": testSecret,
						},
					},
				},
			},
			oldSecretType:      webhookutils.ConnectionTypeOCI.String(),
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasLLMISVCImagePullSecretsCleanupPatch(),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runLLMISVCTestCase(t, tc)
		})
	}
}
