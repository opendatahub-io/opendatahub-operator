package envtestutil

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
)

const DefaultWebhookTimeout = 30 * time.Second

// defaultCRDInstallOptions provides consistent configuration for waiting on CRD establishment.
var defaultCRDInstallOptions = envtest.CRDInstallOptions{
	PollInterval: 100 * time.Millisecond,
	MaxTime:      30 * time.Second,
}

// =============================================================================
// Type Definitions
// =============================================================================

// ObjectOption is a functional option for configuring client objects during creation.
type ObjectOption func(client.Object)

// CRDSetupOption is a functional option for configuring the test environment setup with CRDs.
type CRDSetupOption func(ctx context.Context, t *testing.T, env *envt.EnvT) error

// =============================================================================
// Helper Functions
// =============================================================================

// createAndWaitForCRD creates a CRD and waits for it to be established.
// This helper eliminates code duplication between different CRD setup functions.
func createAndWaitForCRD(ctx context.Context, env *envt.EnvT, crd *apiextensionsv1.CustomResourceDefinition) error {
	extClient, _ := apiextensionsclientset.NewForConfig(env.Config())
	createdCRD, err := extClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create CRD %s: %w", crd.Name, err)
	}

	// Wait for the CRD to be established (ready for use)
	if err == nil { // Only wait if we just created it
		err = envtest.WaitForCRDs(env.Config(), []*apiextensionsv1.CustomResourceDefinition{createdCRD}, defaultCRDInstallOptions)
		if err != nil {
			return fmt.Errorf("failed to wait for CRD %s to be established: %w", crd.Name, err)
		}
	}

	return nil
}

// =============================================================================
// Environment Setup Functions
// =============================================================================

// SetupEnvAndClient sets up an envtest environment for integration tests.
// Parameters:
//   - t: The testing.T object for logging and fatal errors.
//   - registerWebhooks: Functions to register webhooks with the manager.
//   - timeout: The maximum duration to wait for the server to become ready.
//
// Returns:
//   - context.Context: The context for the test environment.
//   - *envt.EnvT: The envtest environment wrapper instance.
//   - func(): A teardown function to clean up resources after the test.
func SetupEnvAndClient(
	t *testing.T,
	registerWebhooks []envt.RegisterWebhooksFn,
	timeout time.Duration,
) (context.Context, *envt.EnvT, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	env, err := envt.New(
		envt.WithRegisterWebhooks(registerWebhooks...),
	)
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}

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
			if err != nil {
				t.Errorf("manager goroutine error: %v", err)
			}
		default:
			// No error
		}
	}

	return ctx, env, teardown
}

// SetupEnvAndClientWithCRDs boots the envtest environment with webhook support and specified CRDs.
// This is a flexible version of SetupEnvAndClient that includes CRD registration based on the options specified.
//
// Parameters:
//   - t: The testing.T object for logging and fatal errors.
//   - registerWebhooks: Functions to register webhooks with the manager.
//   - timeout: The maximum duration to wait for the server to become ready.
//   - opts: Setup options to configure which CRDs to register.
//
// Returns:
//   - context.Context: The context for the test environment.
//   - *envt.EnvT: The envtest environment wrapper instance.
//   - func(): A teardown function to clean up resources after the test.
func SetupEnvAndClientWithCRDs(
	t *testing.T,
	registerWebhooks []envt.RegisterWebhooksFn,
	timeout time.Duration,
	opts ...CRDSetupOption,
) (context.Context, *envt.EnvT, func()) {
	t.Helper()

	// Use the standard envtestutil setup
	ctx, env, teardown := SetupEnvAndClient(t, registerWebhooks, timeout)

	// Register HardwareProfile types (always needed for hardware profile webhook tests)
	if err := hwpv1alpha1.AddToScheme(env.Scheme()); err != nil {
		t.Fatalf("failed to add HardwareProfile types to scheme: %v", err)
	}

	// Apply each option (each option handles its own CRD setup)
	for _, opt := range opts {
		if err := opt(ctx, t, env); err != nil {
			t.Fatalf("failed to apply setup option: %v", err)
		}
	}

	return ctx, env, teardown
}

// =============================================================================
// CRD Setup Options
// =============================================================================

// WithNotebook enables Notebook CRD registration in the test environment.
func WithNotebook() CRDSetupOption {
	return func(ctx context.Context, t *testing.T, env *envt.EnvT) error {
		t.Helper()

		// Register Notebook types
		env.Scheme().AddKnownTypeWithName(gvk.Notebook, &unstructured.Unstructured{})
		env.Scheme().AddKnownTypeWithName(gvk.Notebook.GroupVersion().WithKind("NotebookList"), &unstructured.UnstructuredList{})

		// Create Notebook CRD
		crd := MockNotebookCRD()
		if err := createAndWaitForCRD(ctx, env, crd); err != nil {
			return fmt.Errorf("failed to create and wait for Notebook CRD: %w", err)
		}

		return nil
	}
}

// WithInferenceService enables InferenceService CRD registration in the test environment.
func WithInferenceService() CRDSetupOption {
	return func(ctx context.Context, t *testing.T, env *envt.EnvT) error {
		t.Helper()

		// Register InferenceService types
		env.Scheme().AddKnownTypeWithName(gvk.InferenceServices, &unstructured.Unstructured{})
		env.Scheme().AddKnownTypeWithName(gvk.InferenceServices.GroupVersion().WithKind("InferenceServiceList"), &unstructured.UnstructuredList{})

		// Create InferenceService CRD
		crd := MockInferenceServiceCRD()
		if err := createAndWaitForCRD(ctx, env, crd); err != nil {
			return fmt.Errorf("failed to create and wait for InferenceService CRD: %w", err)
		}

		return nil
	}
}

// =============================================================================
// Object Creation Functions
// =============================================================================

// NewDSCI creates a DSCInitialization object with the given name and namespace for use in tests.
//
// Parameters:
//   - name: The name of the DSCInitialization object.
//   - namespace: The namespace for the object.
//   - opts: Optional functional options to mutate the object.
//
// Returns:
//   - *dsciv1.DSCInitialization: The constructed DSCInitialization object.
func NewDSCI(name, namespace string, opts ...func(*dsciv1.DSCInitialization)) *dsciv1.DSCInitialization {
	dsci := &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.DSCInitialization.Kind,
			APIVersion: dsciv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	for _, opt := range opts {
		opt(dsci)
	}
	return dsci
}

// NewDSC creates a DataScienceCluster object with the given name and namespace for use in tests.
//
// Parameters:
//   - name: The name of the DataScienceCluster object.
//   - namespace: The namespace for the object.
//   - opts: Optional functional options to mutate the object.
//
// Returns:
//   - *dscv1.DataScienceCluster: The constructed DataScienceCluster object.
func NewDSC(name, namespace string, opts ...func(*dscv1.DataScienceCluster)) *dscv1.DataScienceCluster {
	dsc := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.DataScienceCluster.Kind,
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	for _, opt := range opts {
		opt(dsc)
	}
	return dsc
}

// NewAuth creates an Auth object with the given name, namespace, and groups for use in tests.
//
// Parameters:
//   - name: The name of the Auth object.
//   - namespace: The namespace for the object.
//   - adminGroups: The admin groups for the Auth resource.
//   - allowedGroups: The allowed groups for the Auth resource.
//   - opts: Optional functional options to mutate the object.
//
// Returns:
//   - *serviceApi.Auth: The constructed Auth object.
func NewAuth(name, namespace string, adminGroups, allowedGroups []string, opts ...func(*serviceApi.Auth)) *serviceApi.Auth {
	auth := &serviceApi.Auth{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.Auth.Kind,
			APIVersion: serviceApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   adminGroups,
			AllowedGroups: allowedGroups,
		},
	}
	for _, opt := range opts {
		opt(auth)
	}
	return auth
}

// NewHWP creates a HardwareProfile object with the given name and namespace for use in tests.
//
// Parameters:
//   - name: The name of the HardwareProfile object.
//   - namespace: The namespace for the object.
//   - opts: Optional functional options to mutate the object.
//
// Returns:
//   - *hwpv1alpha1.HardwareProfile: The constructed HardwareProfile object.
func NewHWP(name, namespace string, opts ...func(profile *hwpv1alpha1.HardwareProfile)) *hwpv1alpha1.HardwareProfile {
	hwp := &hwpv1alpha1.HardwareProfile{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.HardwareProfile.Kind,
			APIVersion: hwpv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	for _, opt := range opts {
		opt(hwp)
	}
	return hwp
}

// NewNamespace creates a Namespace object with the given name and labels for use in tests.
//
// Parameters:
//   - name: The name of the Namespace object.
//   - labels: The labels to set on the namespace.
//   - opts: Optional functional options to mutate the object.
//
// Returns:
//   - *corev1.Namespace: The constructed Namespace object.
func NewNamespace(name string, labels map[string]string, opts ...func(*corev1.Namespace)) *corev1.Namespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
	for _, opt := range opts {
		opt(ns)
	}
	return ns
}

// NewNotebook creates a Notebook object with the given name and namespace for use in tests.
//
// Parameters:
//   - name: The name of the Notebook object.
//   - namespace: The namespace for the object.
//
// Returns:
//   - client.Object: The constructed Notebook object as an unstructured object.
func NewNotebook(name, namespace string, opts ...ObjectOption) client.Object {
	notebook := resources.GvkToUnstructured(gvk.Notebook)
	notebook.SetName(name)
	notebook.SetNamespace(namespace)

	// Set basic spec structure needed for webhook testing
	containers := []interface{}{
		map[string]interface{}{
			"name":  "notebook",
			"image": "jupyter/base-notebook:latest",
		},
	}
	if err := unstructured.SetNestedSlice(notebook.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		panic(fmt.Sprintf("failed to set notebook containers: %v", err))
	}

	for _, opt := range opts {
		opt(notebook)
	}
	return notebook
}

// NewInferenceService creates an InferenceService object with the given name and namespace for use in tests.
//
// Parameters:
//   - name: The name of the InferenceService object.
//   - namespace: The namespace for the object.
//
// Returns:
//   - client.Object: The constructed InferenceService object as an unstructured object.
func NewInferenceService(name, namespace string, opts ...ObjectOption) client.Object {
	inferenceService := resources.GvkToUnstructured(gvk.InferenceServices)
	inferenceService.SetName(name)
	inferenceService.SetNamespace(namespace)

	// Set basic spec structure needed for webhook testing
	containers := []interface{}{
		map[string]interface{}{
			"name":  "kserve-container",
			"image": "kserve/model-server:latest",
		},
	}
	if err := unstructured.SetNestedSlice(inferenceService.Object, containers, "spec", "predictor", "podSpec", "containers"); err != nil {
		panic(fmt.Sprintf("failed to set inference service containers: %v", err))
	}

	for _, opt := range opts {
		opt(inferenceService)
	}
	return inferenceService
}

// =============================================================================
// Object Configuration Options
// =============================================================================

// WithLabels sets labels on the object.
func WithLabels(labels map[string]string) ObjectOption {
	return func(obj client.Object) {
		obj.SetLabels(labels)
	}
}

// WithHardwareProfile adds a hardware profile annotation to the object.
func WithHardwareProfile(profileName string) ObjectOption {
	return func(obj client.Object) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["opendatahub.io/hardware-profile-name"] = profileName
		obj.SetAnnotations(annotations)
	}
}

// WithHardwareProfileNamespace adds a hardware profile namespace annotation to the object.
func WithHardwareProfileNamespace(namespace string) ObjectOption {
	return func(obj client.Object) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["opendatahub.io/hardware-profile-namespace"] = namespace
		obj.SetAnnotations(annotations)
	}
}

// WithAnnotation adds an annotation to the object.
func WithAnnotation(key, value string) ObjectOption {
	return func(obj client.Object) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[key] = value
		obj.SetAnnotations(annotations)
	}
}

// =============================================================================
// Webhook Helper Functions
// =============================================================================

// NewAdmissionRequest creates an admission request for testing webhooks.
//
// Parameters:
//   - t: The testing.T object for logging and fatal errors.
//   - op: The operation type (Create, Update, Delete).
//   - obj: The object to include in the request.
//   - kind: The GroupVersionKind of the object.
//   - resource: The GroupVersionResource of the object.
//
// Returns:
//   - admission.Request: The constructed admission request.
func NewAdmissionRequest(
	t *testing.T,
	op admissionv1.Operation,
	obj client.Object,
	kind schema.GroupVersionKind,
	resource metav1.GroupVersionResource,
) admission.Request {
	t.Helper()

	objBytes, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal object: %v", err)
	}

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: kind.Group, Version: kind.Version, Kind: kind.Kind},
			Resource:  resource,
			Namespace: obj.GetNamespace(),
			Operation: op,
			Object:    runtime.RawExtension{Raw: objBytes},
		},
	}
}

// =============================================================================
// Mock CRD Functions
// =============================================================================

// MockNotebookCRD creates a mock Notebook CRD for testing.
func MockNotebookCRD() *apiextensionsv1.CustomResourceDefinition {
	preserveUnknownFields := true

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
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						// This allows any structure
						XPreserveUnknownFields: &preserveUnknownFields,
					},
				},
			}},
		},
	}
}

// MockInferenceServiceCRD creates a mock InferenceService CRD for testing.
func MockInferenceServiceCRD() *apiextensionsv1.CustomResourceDefinition {
	preserveUnknownFields := true

	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "inferenceservices.serving.kserve.io",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "serving.kserve.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "inferenceservices",
				Singular: "inferenceservice",
				Kind:     "InferenceService",
			},
			Scope: "Namespaced",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1beta1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						// This allows any structure
						XPreserveUnknownFields: &preserveUnknownFields,
					},
				},
			}},
		},
	}
}

// SetupWebhookConfigurations creates and configures webhook configurations for the given environment.
// Parameters:
//   - t: The testing.T object for error reporting.
//   - env: The envtest environment wrapper instance.
//   - ctx: The context for the test environment.
//
// Returns:
//   - None.
func SetupWebhookConfigurations(t *testing.T, env *envt.EnvT, ctx context.Context) {
	t.Helper()

	// Set env for webhook to work
	//nolint:usetesting
	os.Setenv("ENVTEST_WEBHOOK_LOCAL_PORT", strconv.Itoa(env.Env.WebhookInstallOptions.LocalServingPort))
	//nolint:usetesting
	os.Setenv("ENVTEST_WEBHOOK_LOCAL_CERT_DIR", env.Env.WebhookInstallOptions.LocalServingCertDir)

	vwc := webhook.DesiredValidatingWebhookConfiguration("kueue-webhook-test")
	if err := env.Client().Create(ctx, vwc); err != nil && !k8serr.IsAlreadyExists(err) {
		t.Fatalf("failed to create webhook configuration: %v", err)
	}
}
