package envtestutil

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
)

// WaitForWebhookServer waits until the webhook server is ready by dialing the port using TLS.
//
// Parameters:
//   - host: The host address of the webhook server.
//   - port: The port number of the webhook server.
//   - timeout: The maximum duration to wait for the server to become ready.
//
// Returns:
//   - error: If the server is not ready within the timeout or a connection error occurs.
func WaitForWebhookServer(host string, port int, timeout time.Duration) error {
	addrPort := fmt.Sprintf("%s:%d", host, port)
	dialer := &net.Dialer{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{
			InsecureSkipVerify: true, // #nosec G402
			MinVersion:         tls.VersionTLS12,
		})
		if err == nil {
			return conn.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("webhook server not ready after %s", timeout)
}

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
	if err := WaitForWebhookServer(env.Env.WebhookInstallOptions.LocalServingHost, env.Env.WebhookInstallOptions.LocalServingPort, timeout); err != nil {
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
			APIVersion: gvk.DSCInitialization.Version,
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
			APIVersion: gvk.DataScienceCluster.Version,
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
