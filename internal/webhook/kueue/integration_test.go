package kueue_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

var (
	notebookGVK = schema.GroupVersionKind{
		Group:   "kubeflow.org",
		Version: "v1",
		Kind:    "Notebook",
	}

	kueueQueueNameLabelKey     = kueuewebhook.KueueQueueNameLabelKey
	localQueueName             = "default"
	KueueManagedLabelKey       = "kueue.openshift.io/managed"
	KueueLegacyManagedLabelKey = "kueue-managed"
	missingLabelError          = `Kueue label validation failed: missing required label "` + kueueQueueNameLabelKey + `"`
)

// mockNotebookCRD creates a fake Notebook CRD to allow webhook testing.
func mockNotebookCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "notebooks.kubeflow.org"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "kubeflow.org",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     "notebooks",
				Singular:   "notebook",
				Kind:       "Notebook",
				ShortNames: []string{"nb"},
			},
			Scope: "Namespaced",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				},
			}},
		},
	}
}

// SetupEnvAndClientWithNotebook sets up the environment and client with a mock Notebook CRD.
// Parameters:
//   - t: The testing.T object for error reporting.
//   - registerWebhooks: The list of webhook registration functions to register.
//   - timeout: The timeout duration for the environment setup.
//
// Returns:
//   - context.Context: The context for the test environment.
//   - *envt.EnvT: The environment wrapper instance.
//   - func(): A function to clean up the environment.
//   - client.Client: The client for the test environment.
func SetupEnvAndClientWithNotebook(
	t *testing.T,
	registerWebhooks []envt.RegisterWebhooksFn,
	timeout time.Duration,
) (context.Context, *envt.EnvT, func(), client.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	env, err := envt.New(envt.WithRegisterWebhooks(registerWebhooks...))
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}

	env.Scheme().AddKnownTypeWithName(notebookGVK, &unstructured.Unstructured{})
	env.Scheme().AddKnownTypeWithName(notebookGVK.GroupVersion().WithKind("NotebookList"), &unstructured.UnstructuredList{})

	extClient, _ := apiextensionsclientset.NewForConfig(env.Config())
	_, err = extClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, mockNotebookCRD(), metav1.CreateOptions{})
	if err != nil && !k8serr.IsAlreadyExists(err) {
		t.Fatalf("failed to create mock Notebook CRD: %v", err)
	}

	// Create webhook configuration
	envtestutil.SetupWebhookConfigurations(t, env, ctx)

	mgrCtx, mgrCancel := context.WithCancel(ctx)
	errChan := make(chan error, 1)

	go func() {
		t.Log("Starting manager...")
		if err := env.Manager().Start(mgrCtx); err != nil {
			select {
			case errChan <- fmt.Errorf("manager exited with error: %w", err):
			default:
			}
		}
	}()

	t.Log("Waiting for webhook server to be ready...")
	if err := env.WaitForWebhookServer(timeout); err != nil {
		t.Fatalf("webhook server not ready: %v", err)
	}

	teardown := func() {
		mgrCancel()
		cancel()
		_ = env.Stop()
		select {
		case err := <-errChan:
			t.Errorf("manager goroutine error: %v", err)
		default:
		}
	}

	return ctx, env, teardown, env.Client()
}

// createDSCWithStatus creates a DataScienceCluster with the given management state.
// Parameters:
//   - ctx: The context for the API call.
//   - g: The Gomega test context.
//   - c: The client for the test environment.
//   - state: The management state for the DataScienceCluster.
//
// Returns: None.
func createDSCWithStatus(ctx context.Context, g *WithT, c client.Client, state operatorv1.ManagementState) {
	dsc := &dscv1.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	g.Expect(c.Create(ctx, dsc)).To(Succeed())

	dsc.Status = dscv1.DataScienceClusterStatus{
		Components: dscv1.ComponentsStatus{
			Kueue: componentApi.DSCKueueStatus{
				KueueManagementSpec: componentApi.KueueManagementSpec{
					ManagementState: state,
				},
			},
		},
	}
	g.Expect(c.Status().Update(ctx, dsc)).To(Succeed())
}

// newTestNamespace creates a new test namespace with the given name and labels.
// Parameters:
//   - name: The name of the namespace.
//   - labels: The labels for the namespace.
//
// Returns:
//   - *corev1.Namespace: The new namespace.
func newTestNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// newTestWorkload creates a new test workload with the given name, namespace, GVK, and labels.
// Parameters:
//   - name: The name of the workload.
//   - namespace: The namespace of the workload.
//   - gvk: The GroupVersionKind of the workload.
//   - labels: The labels for the workload.
//
// Returns:
//   - *unstructured.Unstructured: The new workload.
func newTestWorkload(name, namespace string, gvk schema.GroupVersionKind, labels map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	obj.SetLabels(labels)
	return obj
}

func TestKueueWebhook_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		kueueState        operatorv1.ManagementState
		nsLabels          map[string]string
		workloadLabels    map[string]string
		expectAllowed     bool
		expectDeniedError string
	}{
		{
			name:           "Kueue disabled in DSC - should allow",
			kueueState:     operatorv1.Removed,
			nsLabels:       map[string]string{KueueManagedLabelKey: "true"},
			workloadLabels: map[string]string{},
			expectAllowed:  true,
		},
		{
			name:              "Kueue enabled, ns enabled, missing workload label - should deny",
			kueueState:        operatorv1.Managed,
			nsLabels:          map[string]string{KueueManagedLabelKey: "true"},
			workloadLabels:    map[string]string{},
			expectAllowed:     false,
			expectDeniedError: missingLabelError,
		},
		{
			name:           "Kueue enabled, ns enabled, valid workload label - should allow",
			kueueState:     operatorv1.Managed,
			nsLabels:       map[string]string{KueueManagedLabelKey: "true"},
			workloadLabels: map[string]string{kueueQueueNameLabelKey: localQueueName},
			expectAllowed:  true,
		},
		{
			name:           "Kueue enabled, ns not labeled - should allow",
			kueueState:     operatorv1.Managed,
			nsLabels:       nil,
			workloadLabels: map[string]string{},
			expectAllowed:  true,
		},
		{
			name:           "Kueue enabled, ns enabled with legacy label, valid workload label - should allow",
			kueueState:     operatorv1.Managed,
			nsLabels:       map[string]string{KueueLegacyManagedLabelKey: "true"},
			workloadLabels: map[string]string{kueueQueueNameLabelKey: localQueueName},
			expectAllowed:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, _, teardown, k8sClient := SetupEnvAndClientWithNotebook(
				t,
				[]envt.RegisterWebhooksFn{
					kueuewebhook.RegisterWebhooks,
					dscwebhook.RegisterWebhooks,
				},
				20*time.Second,
			)
			t.Cleanup(teardown)

			ns := xid.New().String()
			createDSCWithStatus(ctx, g, k8sClient, tc.kueueState)
			g.Expect(k8sClient.Create(ctx, newTestNamespace(ns, tc.nsLabels))).To(Succeed())

			workload := newTestWorkload("test-notebook", ns, notebookGVK, tc.workloadLabels)
			err := k8sClient.Create(ctx, workload)

			if tc.expectAllowed {
				g.Expect(err).To(Succeed(), fmt.Sprintf("Expected creation to be allowed but got: %v", err))
			} else {
				g.Expect(err).To(HaveOccurred(), "Expected creation to be denied but it was allowed.")
				statusErr := &k8serr.StatusError{}
				ok := errors.As(err, &statusErr)
				g.Expect(ok).To(BeTrue(), "Expected error to be of type StatusError")
				g.Expect(statusErr.Status().Code).To(Equal(int32(http.StatusForbidden)))
				g.Expect(statusErr.Status().Message).To(ContainSubstring(tc.expectDeniedError))
			}
		})
	}

	os.Unsetenv("ENVTEST_WEBHOOK_LOCAL_PORT")
	os.Unsetenv("ENVTEST_WEBHOOK_LOCAL_CERT_DIR")
}
