package envtestutil

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
)

const DefaultWebhookTimeout = 30 * time.Second

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
			APIVersion: gvk.HardwareProfile.Version,
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

// NewAdmissionRequest constructs an admission.Request for a given object, operation, kind (as schema.GroupVersionKind), and resource.
// It fails the test if marshalling the object fails.
//
// Parameters:
//   - t: The testing.T object for error reporting.
//   - op: The admissionv1.Operation (e.g., Create, Delete).
//   - obj: The client.Object to include in the request.
//   - kind: The schema.GroupVersionKind of the object.
//   - resource: The metav1.GroupVersionResource for the request.
//
// Returns:
//   - admission.Request: The constructed admission request for use in tests.
func NewAdmissionRequest(
	t *testing.T,
	op admissionv1.Operation,
	obj client.Object,
	kind schema.GroupVersionKind,
	resource metav1.GroupVersionResource,
) admission.Request {
	t.Helper()

	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal object: %v", err)
	}
	metaObj, ok := obj.(metav1.Object)
	if !ok {
		t.Fatalf("object does not implement metav1.Object")
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: op,
			Namespace: metaObj.GetNamespace(),
			Name:      metaObj.GetName(),
			Kind: metav1.GroupVersionKind{
				Group:   kind.Group,
				Version: kind.Version,
				Kind:    kind.Kind,
			},
			Resource: resource,
			Object:   runtime.RawExtension{Raw: raw},
		},
	}
}
