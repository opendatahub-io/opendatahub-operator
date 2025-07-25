package inferenceservice_test

import (
	"context"
	"encoding/json"
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
)

type TestCase struct {
	name               string
	secretType         string
	secretData         map[string][]byte
	secretNamespace    string
	annotations        map[string]string
	modelSpec          map[string]interface{}
	operation          admissionv1.Operation
	expectedAllowed    bool
	expectedMessage    string
	expectedPatchCheck func([]jsonpatch.JsonPatchOperation) bool
}

func setupTestEnvironment(t *testing.T) (*runtime.Scheme, context.Context) {
	t.Helper()
	sch, err := scheme.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	return sch, context.Background()
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

func createTestInferenceService(name, namespace string, annotations map[string]string, modelSpec map[string]interface{}) *unstructured.Unstructured {
	isvc := envtestutil.NewInferenceService(name, namespace)
	unstructuredISVC, ok := isvc.(*unstructured.Unstructured)
	if !ok {
		panic("failed to cast InferenceService to unstructured")
	}

	if annotations != nil {
		unstructuredISVC.SetAnnotations(annotations)
	}
	if modelSpec != nil {
		// Ensure the predictor spec exists before setting the model.
		predictorSpec := map[string]interface{}{
			"model": modelSpec,
		}
		if err := unstructured.SetNestedMap(unstructuredISVC.Object, predictorSpec, "spec", "predictor"); err != nil {
			panic("failed to set nested map: " + err.Error())
		}
	}
	return unstructuredISVC
}

func runTestCase(t *testing.T, tc TestCase) {
	t.Helper()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	var cli client.Client
	if tc.secretType != "" {
		secret := createTestSecret(testSecret, tc.secretNamespace, tc.secretType, tc.secretData)
		cli = fake.NewClientBuilder().WithScheme(sch).WithObjects(secret).Build()
	} else {
		cli = fake.NewClientBuilder().WithScheme(sch).Build()
	}

	webhook := createWebhook(cli, sch)

	isvc := createTestInferenceService(testInferenceService, testNamespace, tc.annotations, tc.modelSpec)
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

	if tc.expectedMessage != "" {
		g.Expect(resp.Result.Message).To(ContainSubstring(tc.expectedMessage))
	}

	if tc.expectedPatchCheck != nil {
		g.Expect(tc.expectedPatchCheck(resp.Patches)).To(BeTrue())
	}
}

// oci-v.
func hasImagePullSecretsPatch(patches []jsonpatch.JsonPatchOperation) bool {
	for _, patch := range patches {
		if patch.Path == "/spec/predictor/imagePullSecrets" {
			return true
		}
	}
	return false
}

// uri-v1.
func hasStorageUriPatch(patches []jsonpatch.JsonPatchOperation) bool {
	for _, patch := range patches {
		if patch.Path == "/spec/predictor/model/storageUri" {
			return true
		}
	}
	return false
}

// s3.
func hasStorageKeyPatch(patches []jsonpatch.JsonPatchOperation) bool {
	for _, patch := range patches {
		if patch.Path == "/spec/predictor/model/storage/key" {
			return true
		}
		if patch.Path == "/spec/predictor/model/storage" {
			if storageMap, ok := patch.Value.(map[string]interface{}); ok {
				if _, hasKey := storageMap["key"]; hasKey {
					return true
				}
			}
		}
	}
	return false
}

func TestConnectionWebhook(t *testing.T) {
	testCases := []TestCase{
		{
			name:            "no connection annotation set on ISVC should be allowed to create",
			secretType:      "",
			annotations:     nil,
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "no injection needed",
		},
		{
			name:            "delete operation on allowed type",
			secretType:      "s3",
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Delete,
			expectedAllowed: true,
			expectedMessage: "Operation DELETE",
		},
		{
			name:            "unsupported type set in the annoation should be allow to create but no injection",
			secretType:      "new-type",
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			expectedMessage: "no injection needed",
		},
		{
			name:               "annotation as OCI type, creation allowed with injection",
			secretType:         "oci-v1",
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch,
		},
		{
			name:               "annotation as URI type with model in spec, creation allowed with injection",
			secretType:         "uri-v1",
			secretNamespace:    testNamespace,
			secretData:         map[string][]byte{"URI": []byte("https://opendathub.io/model")},
			annotations:        map[string]string{annotations.Connection: testSecret},
			modelSpec:          map[string]interface{}{},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageUriPatch,
		},
		{
			name:               "annotation as S3 type, creation allowed with injection",
			secretType:         "s3",
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			modelSpec:          map[string]interface{}{},
			operation:          admissionv1.Create,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch,
		},
		{
			name:               "S3 type with existing storageUri in model, update allowed with injection",
			secretType:         "s3",
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			modelSpec:          map[string]interface{}{"storageUri": "s3://existing-bucket/model"},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasStorageKeyPatch,
		},
		{
			name:            "S3 type without model set should not be allowed to create",
			secretType:      "s3",
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "found no spec.predictor.model set in resource",
		},
		{
			name:            "URI type without data.URI set should not be allowed to create",
			secretType:      "uri-v1",
			secretNamespace: testNamespace,
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "secret does not contain 'URI' data key",
		},
		{
			name:            "secret not found regardless not exist or in a different namespace should not be allowed",
			secretType:      "",
			annotations:     map[string]string{annotations.Connection: testSecret},
			operation:       admissionv1.Create,
			expectedAllowed: false,
			expectedMessage: "not found",
		},
		{
			name:               "update operation on any allowed type",
			secretType:         "oci-v1",
			secretNamespace:    testNamespace,
			annotations:        map[string]string{annotations.Connection: testSecret},
			operation:          admissionv1.Update,
			expectedAllowed:    true,
			expectedPatchCheck: hasImagePullSecretsPatch,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runTestCase(t, tc)
		})
	}
}
