package notebook_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	notebookwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/notebook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

func TestNotebookWebhook_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                 string
		setupSecrets         []string
		connectionAnnotation string
		expectAllowed        bool
		expectDeniedError    string
		validateInjection    bool
	}{
		{
			name:                 "no connection annotation - should allow",
			setupSecrets:         nil,
			connectionAnnotation: "",
			expectAllowed:        true,
			validateInjection:    false,
		},
		{
			name:                 "empty connection annotation - should allow",
			setupSecrets:         nil,
			connectionAnnotation: "   ", // whitespace only
			expectAllowed:        true,
			validateInjection:    false,
		},
		{
			name:                 "invalid annotation format - missing namespace - should deny",
			setupSecrets:         nil,
			connectionAnnotation: "invalid-format",
			expectAllowed:        false,
			expectDeniedError:    "failed to parse connections annotation",
		},
		{
			name:                 "invalid annotation format - empty name - should deny",
			setupSecrets:         nil,
			connectionAnnotation: "test-namespace/",
			expectAllowed:        false,
			expectDeniedError:    "failed to parse connections annotation",
		},
		{
			name:                 "invalid annotation format - empty namespace - should deny",
			setupSecrets:         nil,
			connectionAnnotation: "/secret-name",
			expectAllowed:        false,
			expectDeniedError:    "failed to parse connections annotation",
		},
		{
			name:                 "valid single secret - should allow and inject",
			setupSecrets:         []string{"my-connection-secret"},
			connectionAnnotation: "NAMESPACE/my-connection-secret", // NAMESPACE will be replaced
			expectAllowed:        true,
			validateInjection:    true,
		},
		{
			name:                 "valid multiple secrets - should allow and inject",
			setupSecrets:         []string{"secret-1", "secret-2"},
			connectionAnnotation: "NAMESPACE/secret-1,NAMESPACE/secret-2", // NAMESPACE will be replaced
			expectAllowed:        true,
			validateInjection:    true,
		},
		{
			name:                 "mixed valid and invalid secrets - should deny",
			setupSecrets:         []string{"secret-1", "secret-2"},
			connectionAnnotation: "BAD-NS/secret-1,NAMESPACE/secret-2", // NAMESPACE will be replaced
			expectAllowed:        false,
			expectDeniedError:    "some of the connection secret(s) do not exist",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			// Register both notebook webhook and hardware profile webhook to avoid missing webhook error
			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{
					envtestutil.RegisterWebhooks,
				},
				[]envt.RegisterControllersFn{},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithNotebook(),
			)
			t.Cleanup(teardown)

			k8sClient := env.Client()
			ns := xid.New().String()

			g.Expect(k8sClient.Create(ctx, envtestutil.NewNamespace(ns, map[string]string{}))).To(Succeed())

			// Create secrets if specified in test case
			for _, secretName := range tc.setupSecrets {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: ns,
					},
					Type: corev1.SecretTypeOpaque,
					Data: map[string][]byte{
						"connection-url": []byte("https://example.com"),
						"username":       []byte("testuser"),
						"password":       []byte("testpass"),
					},
				}
				g.Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			}

			// Create notebook with connection annotation (if specified)
			notebookName := "test-notebook"
			var notebook client.Object
			if tc.connectionAnnotation != "" {
				// Replace NAMESPACE placeholder with actual namespace
				connectionValue := strings.ReplaceAll(tc.connectionAnnotation, "NAMESPACE", ns)
				notebook = envtestutil.NewNotebook(notebookName, ns,
					envtestutil.WithAnnotation(annotations.Connection, connectionValue),
				)
			} else {
				notebook = envtestutil.NewNotebook(notebookName, ns)
			}

			// Attempt to create the notebook
			err := k8sClient.Create(ctx, notebook)

			if tc.expectAllowed {
				g.Expect(err).To(Succeed(), fmt.Sprintf("Expected creation to be allowed but got: %v", err))

				if tc.validateInjection {
					// Verify injection occurred by checking the created notebook
					createdNotebook := &unstructured.Unstructured{}
					createdNotebook.SetAPIVersion("kubeflow.org/v1")
					createdNotebook.SetKind("Notebook")

					key := types.NamespacedName{Name: notebookName, Namespace: ns}
					g.Expect(k8sClient.Get(ctx, key, createdNotebook)).To(Succeed())

					// Verify that secrets were injected as envFrom entries
					containers, found, injectionErr := unstructured.NestedSlice(createdNotebook.Object, notebookwebhook.NotebookContainersPath...)
					g.Expect(injectionErr).ShouldNot(HaveOccurred())
					g.Expect(found).Should(BeTrue())
					g.Expect(containers).Should(HaveLen(1))

					container, ok := containers[0].(map[string]interface{})
					g.Expect(ok).Should(BeTrue())

					envFrom, found, envFromErr := unstructured.NestedSlice(container, "envFrom")
					g.Expect(envFromErr).ShouldNot(HaveOccurred())
					g.Expect(found).Should(BeTrue())
					g.Expect(envFrom).Should(HaveLen(len(tc.setupSecrets)), "Expected number of envFrom entries to match number of secrets")

					// Verify each secret is referenced
					secretNames := make([]string, 0, len(envFrom))
					for _, entry := range envFrom {
						entryMap, ok := entry.(map[string]interface{})
						g.Expect(ok).Should(BeTrue())

						secretRef, found, err := unstructured.NestedStringMap(entryMap, "secretRef")
						g.Expect(err).ShouldNot(HaveOccurred())
						g.Expect(found).Should(BeTrue())

						secretName, found := secretRef["name"]
						g.Expect(found).Should(BeTrue())
						secretNames = append(secretNames, secretName)
					}

					g.Expect(secretNames).Should(ContainElements(tc.setupSecrets))
				}
			} else {
				g.Expect(err).To(HaveOccurred(), "Expected creation to be denied but it was allowed")
				statusErr := &k8serr.StatusError{}
				ok := errors.As(err, &statusErr)
				g.Expect(ok).To(BeTrue(), "Expected error to be of type StatusError")
				g.Expect(statusErr.Status().Code).To(Equal(int32(http.StatusForbidden)))
				g.Expect(statusErr.Status().Message).To(ContainSubstring(tc.expectDeniedError))
			}
		})
	}
}
