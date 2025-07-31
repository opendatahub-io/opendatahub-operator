package notebook_test

import (
	"context"
	"fmt"
	"testing"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/notebook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	testNamespace    = "test-namespace"
	testNotebook     = "test-notebook"
	testSecret1      = "secret1"
	testSecret2      = "secret2"
	addOperation     = "add"
	replaceOperation = "replace"
)

// Helper function to create a test webhook.
func createTestWebhook(t *testing.T, cli client.Client) *notebook.NotebookWebhook {
	t.Helper()
	g := NewWithT(t)

	sch, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	return &notebook.NotebookWebhook{
		Client:    cli,
		APIReader: cli,
		Decoder:   admission.NewDecoder(sch),
		Name:      "test-webhook",
	}
}

// Helper function to create a basic notebook object.
func createNotebook(options ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	notebook := &unstructured.Unstructured{}
	notebook.SetGroupVersionKind(gvk.Notebook)
	notebook.SetName(testNotebook)
	notebook.SetNamespace(testNamespace)

	spec := map[string]interface{}{
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  "notebook",
						"image": "notebook:latest",
					},
				},
			},
		},
	}
	notebook.Object["spec"] = spec

	for _, opt := range options {
		opt(notebook)
	}

	return notebook
}

// Helper function to add annotations to a notebook.
func withAnnotations(annotations map[string]string) func(*unstructured.Unstructured) {
	return func(notebook *unstructured.Unstructured) {
		notebook.SetAnnotations(annotations)
	}
}

// Helper function to add existing envFrom to a notebook.
func withExistingEnvFrom(envFrom []interface{}) func(*unstructured.Unstructured) {
	return func(nb *unstructured.Unstructured) {
		containers, _, _ := unstructured.NestedSlice(nb.Object, notebook.NotebookContainersPath...)
		if len(containers) > 0 {
			if container, ok := containers[0].(map[string]interface{}); ok {
				container["envFrom"] = envFrom
				containers[0] = container
				_ = unstructured.SetNestedSlice(nb.Object, containers, notebook.NotebookContainersPath...)
			}
		}
	}
}

// Helper function to create admission request.
func createAdmissionRequest(t *testing.T, operation admissionv1.Operation, obj *unstructured.Unstructured) admission.Request {
	t.Helper()
	return envtestutil.NewAdmissionRequest(
		t,
		operation,
		obj,
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)
}

// Helper struct to mock SubjectAccessReview behavior.
type mockClient struct {
	client.Client
	allowPermissions map[string]bool
}

func (m *mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if sar, ok := obj.(*authorizationv1.SubjectAccessReview); ok {
		secretName := sar.Spec.ResourceAttributes.Name
		allowed, exists := m.allowPermissions[secretName]
		sar.Status = authorizationv1.SubjectAccessReviewStatus{
			Allowed: exists && allowed,
		}
		if !allowed {
			sar.Status.Reason = "insufficient permissions"
		}
		return nil
	}
	return m.Client.Create(ctx, obj, opts...)
}

func TestNotebookWebhook_Handle_BasicValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		annotations        map[string]string
		expectedAllowed    bool
		expectedMessage    string
		expectedPatchesLen int
	}{
		{
			name:               "no annotations",
			annotations:        nil,
			expectedAllowed:    true,
			expectedMessage:    "no injection needed",
			expectedPatchesLen: 0,
		},
		{
			name: "empty connections annotation",
			annotations: map[string]string{
				annotations.Connection: "",
			},
			expectedAllowed:    true,
			expectedMessage:    "no injection needed",
			expectedPatchesLen: 0,
		},
		{
			name: "invalid annotation format - missing namespace",
			annotations: map[string]string{
				annotations.Connection: "invalid-format",
			},
			expectedAllowed:    false,
			expectedMessage:    "failed to parse connections annotation",
			expectedPatchesLen: 0,
		},
		{
			name: "invalid annotation format - empty name",
			annotations: map[string]string{
				annotations.Connection: fmt.Sprintf("%s/", testNamespace),
			},
			expectedAllowed:    false,
			expectedMessage:    "failed to parse connections annotation",
			expectedPatchesLen: 0,
		},
		{
			name: "invalid annotation format - empty namespace",
			annotations: map[string]string{
				annotations.Connection: fmt.Sprintf("/%s", testSecret1),
			},
			expectedAllowed:    false,
			expectedMessage:    "failed to parse connections annotation",
			expectedPatchesLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			cli := fake.NewClientBuilder().Build()
			webhook := createTestWebhook(t, cli)

			notebook := createNotebook(withAnnotations(tt.annotations))
			req := createAdmissionRequest(t, admissionv1.Create, notebook)

			resp := webhook.Handle(context.Background(), req)

			g.Expect(resp.Allowed).Should(Equal(tt.expectedAllowed))
			g.Expect(resp.Patches).Should(HaveLen(tt.expectedPatchesLen))
			g.Expect(resp.Result.Message).Should(ContainSubstring(tt.expectedMessage))
		})
	}
}

func TestNotebookWebhook_Handle_Permissions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		connections       string
		allowPermissions  map[string]bool
		expectedAllowed   bool
		expectedMessage   string
		shouldHavePatches bool
		forbiddenSecrets  []string
		secretsToCreate   []string
	}{
		{
			name:        "successful injection with single secret",
			connections: fmt.Sprintf("%s/%s", testNamespace, testSecret1),
			allowPermissions: map[string]bool{
				testSecret1: true,
			},
			expectedAllowed:   true,
			shouldHavePatches: true,
			secretsToCreate:   []string{testSecret1},
		},
		{
			name:        "permission denied for single secret",
			connections: fmt.Sprintf("%s/%s", testNamespace, testSecret1),
			allowPermissions: map[string]bool{
				testSecret1: false,
			},
			expectedAllowed:   false,
			expectedMessage:   "user does not have permission to access the following connection secret(s)",
			shouldHavePatches: false,
			forbiddenSecrets:  []string{fmt.Sprintf("%s/%s", testNamespace, testSecret1)},
			secretsToCreate:   []string{testSecret1},
		},
		{
			name:        "mixed permissions for multiple secrets",
			connections: fmt.Sprintf("%s/%s,%s/%s", testNamespace, testSecret1, testNamespace, testSecret2),
			allowPermissions: map[string]bool{
				testSecret1: true,
				testSecret2: false,
			},
			expectedAllowed:   false,
			expectedMessage:   "user does not have permission to access the following connection secret(s)",
			shouldHavePatches: false,
			forbiddenSecrets:  []string{fmt.Sprintf("%s/%s", testNamespace, testSecret2)},
			secretsToCreate:   []string{testSecret1, testSecret2},
		},
		{
			name:        "secret does not exist",
			connections: fmt.Sprintf("%s/%s,%s/%s", testNamespace, testSecret1, testNamespace, testSecret2),
			allowPermissions: map[string]bool{
				testSecret1: false,
				testSecret2: false,
			},
			expectedAllowed:   false,
			expectedMessage:   "some of the connection secret(s) do not exist",
			shouldHavePatches: false,
			forbiddenSecrets:  []string{fmt.Sprintf("%s/%s", testNamespace, testSecret2)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			baseCli := fake.NewClientBuilder().Build()
			cli := &mockClient{
				Client:           baseCli,
				allowPermissions: tt.allowPermissions,
			}

			for _, secretName := range tt.secretsToCreate {
				g.Expect(cli.Create(context.Background(), &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: testNamespace,
					},
				})).Should(Succeed())
			}

			webhook := createTestWebhook(t, cli)

			notebook := createNotebook(withAnnotations(map[string]string{
				annotations.Connection: tt.connections,
			}))
			req := createAdmissionRequest(t, admissionv1.Create, notebook)

			resp := webhook.Handle(context.Background(), req)

			g.Expect(resp.Allowed).Should(Equal(tt.expectedAllowed))

			if tt.shouldHavePatches {
				g.Expect(resp.Patches).ShouldNot(BeEmpty())
			} else {
				g.Expect(resp.Patches).Should(BeEmpty())
			}

			if tt.expectedMessage != "" {
				g.Expect(resp.Result.Message).Should(ContainSubstring(tt.expectedMessage))
			}

			for _, forbidden := range tt.forbiddenSecrets {
				g.Expect(resp.Result.Message).Should(ContainSubstring(forbidden))
			}
		})
	}
}

func TestNotebookWebhook_Handle_Operations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		operation         admissionv1.Operation
		expectedAllowed   bool
		expectedMessage   string
		shouldHavePatches bool
	}{
		{
			name:              "create operation with valid permissions",
			operation:         admissionv1.Create,
			expectedAllowed:   true,
			shouldHavePatches: true,
		},
		{
			name:              "update operation with valid permissions",
			operation:         admissionv1.Update,
			expectedAllowed:   true,
			shouldHavePatches: true,
		},
		{
			name:              "delete operation",
			operation:         admissionv1.Delete,
			expectedAllowed:   true,
			expectedMessage:   "Operation DELETE on Notebook allowed",
			shouldHavePatches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			baseCli := fake.NewClientBuilder().Build()
			cli := &mockClient{
				Client: baseCli,
				allowPermissions: map[string]bool{
					testSecret1: true,
				},
			}

			g.Expect(cli.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecret1,
					Namespace: testNamespace,
				},
			})).Should(Succeed())

			webhook := createTestWebhook(t, cli)

			notebook := createNotebook(withAnnotations(map[string]string{
				annotations.Connection: fmt.Sprintf("%s/%s", testNamespace, testSecret1),
			}))
			req := createAdmissionRequest(t, tt.operation, notebook)

			resp := webhook.Handle(context.Background(), req)

			g.Expect(resp.Allowed).Should(Equal(tt.expectedAllowed))

			if tt.shouldHavePatches {
				g.Expect(resp.Patches).ShouldNot(BeEmpty())
			} else {
				g.Expect(resp.Patches).Should(BeEmpty())
			}

			if tt.expectedMessage != "" {
				g.Expect(resp.Result.Message).Should(ContainSubstring(tt.expectedMessage))
			}
		})
	}
}

func TestNotebookWebhook_Handle_EnvFromInjection(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	baseCli := fake.NewClientBuilder().Build()
	cli := &mockClient{
		Client: baseCli,
		allowPermissions: map[string]bool{
			testSecret1: true,
			testSecret2: true,
		},
	}

	g.Expect(cli.Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSecret1,
			Namespace: testNamespace,
		},
	})).Should(Succeed())
	g.Expect(cli.Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSecret2,
			Namespace: testNamespace,
		},
	})).Should(Succeed())

	webhook := createTestWebhook(t, cli)

	// Helper function to check if a patch value contains a secretRef with the given name
	containsSecretRef := func(value interface{}, secretName string) bool {
		if envFromArray, ok := value.([]interface{}); ok {
			for _, entry := range envFromArray {
				if entryMap, ok := entry.(map[string]interface{}); ok {
					if secretRef, hasSecret := entryMap["secretRef"]; hasSecret {
						if secretRefMap, ok := secretRef.(map[string]interface{}); ok {
							if name, hasName := secretRefMap["name"]; hasName && name == secretName {
								return true
							}
						}
					}
				}
			}
		} else if entryMap, ok := value.(map[string]interface{}); ok {
			if secretRef, hasSecret := entryMap["secretRef"]; hasSecret {
				if secretRefMap, ok := secretRef.(map[string]interface{}); ok {
					if name, hasName := secretRefMap["name"]; hasName && name == secretName {
						return true
					}
				}
			}
		}
		return false
	}

	tests := []struct {
		name           string
		notebook       *unstructured.Unstructured
		expectedChecks []func(jsonpatch.JsonPatchOperation) bool
	}{
		{
			name: "inject single secret",
			notebook: createNotebook(withAnnotations(map[string]string{
				annotations.Connection: fmt.Sprintf("%s/%s", testNamespace, testSecret1),
			})),
			expectedChecks: []func(jsonpatch.JsonPatchOperation) bool{
				func(patch jsonpatch.JsonPatchOperation) bool {
					return patch.Operation == addOperation &&
						patch.Path == "/spec/template/spec/containers/0/envFrom" &&
						containsSecretRef(patch.Value, testSecret1)
				},
			},
		},
		{
			name: "inject multiple secrets",
			notebook: createNotebook(withAnnotations(map[string]string{
				annotations.Connection: fmt.Sprintf("%s/%s,%s/%s", testNamespace, testSecret1, testNamespace, testSecret2),
			})),
			expectedChecks: []func(jsonpatch.JsonPatchOperation) bool{
				func(patch jsonpatch.JsonPatchOperation) bool {
					return patch.Operation == addOperation &&
						patch.Path == "/spec/template/spec/containers/0/envFrom" &&
						containsSecretRef(patch.Value, testSecret1) &&
						containsSecretRef(patch.Value, testSecret2)
				},
			},
		},
		{
			name: "preserve existing configMapRef and inject secret",
			notebook: createNotebook(
				withAnnotations(map[string]string{
					annotations.Connection: fmt.Sprintf("%s/%s", testNamespace, testSecret1),
				}),
				withExistingEnvFrom([]interface{}{
					map[string]interface{}{
						"configMapRef": map[string]interface{}{
							"name": "existing-config",
						},
					},
				}),
			),
			expectedChecks: []func(jsonpatch.JsonPatchOperation) bool{
				func(patch jsonpatch.JsonPatchOperation) bool {
					return patch.Operation == addOperation &&
						patch.Path == "/spec/template/spec/containers/0/envFrom/1" &&
						containsSecretRef(patch.Value, testSecret1)
				},
			},
		},
		{
			name: "replace existing secretRef with new one",
			notebook: createNotebook(
				withAnnotations(map[string]string{
					annotations.Connection: fmt.Sprintf("%s/%s", testNamespace, testSecret1),
				}),
				withExistingEnvFrom([]interface{}{
					map[string]interface{}{
						"secretRef": map[string]interface{}{
							"name": "old-secret",
						},
					},
				}),
			),
			expectedChecks: []func(jsonpatch.JsonPatchOperation) bool{
				func(patch jsonpatch.JsonPatchOperation) bool {
					return patch.Operation == replaceOperation &&
						patch.Path == "/spec/template/spec/containers/0/envFrom/0/secretRef/name" &&
						patch.Value == testSecret1
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			req := createAdmissionRequest(t, admissionv1.Create, tt.notebook)
			resp := webhook.Handle(context.Background(), req)

			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).ShouldNot(BeEmpty())

			verifyExpectedPatches(t, resp.Patches, tt.expectedChecks)
		})
	}
}

// Helper function to verify expected patches.
func verifyExpectedPatches(t *testing.T, actualPatches []jsonpatch.JsonPatchOperation, expectedPatchChecks []func(jsonpatch.JsonPatchOperation) bool) {
	t.Helper()
	g := NewWithT(t)

	g.Expect(actualPatches).Should(HaveLen(len(expectedPatchChecks)), "Expected number of patches to match")

	for i, check := range expectedPatchChecks {
		g.Expect(check(actualPatches[i])).Should(BeTrue(), fmt.Sprintf("Patch %d failed validation", i))
	}
}
