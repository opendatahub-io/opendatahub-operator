//nolint:testpackage
package reconciler

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	gomegaTypes "github.com/onsi/gomega/types"
	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

const testHostURL = "https://127.0.0.1:1"

const errDiscoveryClientCached = "Discovery client should be cached"
const errDynamicClientCached = "Dynamic client should be cached"
const fastFailTimeout = 50 * time.Millisecond

const (
	errNameCannotBeEmpty  = "name cannot be empty"
	errManagerCannotBeNil = "manager cannot be nil"
)

// Specific mock method constants for commonly used methods.
const (
	mockGetNotImplemented                          = "Get not implemented in mock"
	mockListNotImplemented                         = "List not implemented in mock"
	mockCreateNotImplemented                       = "Create not implemented in mock"
	mockDeleteNotImplemented                       = "Delete not implemented in mock"
	mockUpdateNotImplemented                       = "Update not implemented in mock"
	mockPatchNotImplemented                        = "Patch not implemented in mock"
	mockDeleteAllOfNotImplemented                  = "DeleteAllOf not implemented in mock"
	mockStatusNotImplemented                       = "Status not implemented in mock"
	mockIsObjectNamespacedNotImplemented           = "IsObjectNamespaced not implemented in mock"
	mockRESTMapperNotImplemented                   = "RESTMapper not implemented in mock"
	mockSubResourceNotImplemented                  = "SubResource not implemented in mock"
	mockGetFieldIndexerNotImplemented              = "GetFieldIndexer not implemented in mock"
	mockGetCacheNotImplemented                     = "GetCache not implemented in mock"
	mockGetEventRecorderForNotImplemented          = "GetEventRecorderFor not implemented in mock"
	mockGetRESTMapperNotImplemented                = "GetRESTMapper not implemented in mock"
	mockGetAPIReaderNotImplemented                 = "GetAPIReader not implemented in mock"
	mockGetWebhookServerNotImplemented             = "GetWebhookServer not implemented in mock"
	mockGetLoggerNotImplemented                    = "GetLogger not implemented in mock"
	mockGetControllerOptionsNotImplemented         = "GetControllerOptions not implemented in mock"
	mockAddNotImplemented                          = "Add not implemented in mock"
	mockElectLeaderNotImplemented                  = "ElectLeader not implemented in mock"
	mockElectedNotImplemented                      = "Elected not implemented in mock"
	mockGetHTTPClientNotImplemented                = "GetHTTPClient not implemented in mock"
	mockAddMetricsExtraHandlerNotImplemented       = "AddMetricsExtraHandler not implemented in mock"
	mockAddMetricsServerExtraHandlerNotImplemented = "AddMetricsServerExtraHandler not implemented in mock"
	mockAddHealthzCheckNotImplemented              = "AddHealthzCheck not implemented in mock"
	mockAddReadyzCheckNotImplemented               = "AddReadyzCheck not implemented in mock"
	mockStartNotImplemented                        = "Start not implemented in mock"
)

func createEnvTest(s *runtime.Scheme) (*envtest.Environment, error) {
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
	utilruntime.Must(dscv1.AddToScheme(s))
	utilruntime.Must(dsciv1.AddToScheme(s))

	projectDir, err := envtestutil.FindProjectRoot()
	if err != nil {
		return nil, err
	}

	envTest := envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: s,
			Paths: []string{
				filepath.Join(projectDir, "config", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	return &envTest, nil
}

func createReconciler(cli client.Client) *Reconciler {
	return &Reconciler{
		Client:   cli,
		Scheme:   cli.Scheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("test"),
		Release:  cluster.GetRelease(),
		Recorder: record.NewFakeRecorder(100),
		name:     "test",
		instanceFactory: func() (common.PlatformObject, error) {
			i := &componentApi.Dashboard{
				TypeMeta: ctrl.TypeMeta{
					APIVersion: gvk.Dashboard.GroupVersion().String(),
					Kind:       gvk.Dashboard.Kind,
				},
			}

			return i, nil
		},
		conditionsManagerFactory: func(accessor common.ConditionsAccessor) *conditions.Manager {
			return conditions.NewManager(accessor, status.ConditionTypeReady)
		},
	}
}

func TestConditions(t *testing.T) {
	ctx := t.Context()

	g := NewWithT(t)
	s := runtime.NewScheme()

	envTest, err := createEnvTest(s)
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	dsci := resources.GvkToUnstructured(gvk.DSCInitialization)
	dsci.SetName(xid.New().String())
	dsci.SetGeneration(1)

	err = cli.Create(ctx, dsci)
	g.Expect(err).NotTo(HaveOccurred())

	tests := []struct {
		name    string
		err     error
		matcher gomegaTypes.GomegaMatcher
	}{
		{
			name: "ready",
			err:  nil,

			matcher: And(
				jq.Match(`all(.status.conditions[]?.type; . != "foo")`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
			),
		},
		{
			name: "stop",
			err:  odherrors.NewStopError("stop"),
			matcher: And(
				jq.Match(`all(.status.conditions[]?.type; . != "foo")`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
			),
		},
		{
			name: "failure",
			err:  errors.New("failure"),
			matcher: And(
				jq.Match(`all(.status.conditions[]?.type; . != "foo")`),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dash := resources.GvkToUnstructured(gvk.Dashboard)
			dash.SetName(componentApi.DashboardInstanceName)
			dash.SetGeneration(1)

			err = cli.Create(ctx, dash)
			g.Expect(err).NotTo(HaveOccurred())

			st, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&common.Status{
				Conditions: []common.Condition{{
					Type:               "foo",
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.NewTime(time.Now()),
				}},
			})

			g.Expect(err).NotTo(HaveOccurred())

			err = unstructured.SetNestedField(dash.Object, st, "status")
			g.Expect(err).NotTo(HaveOccurred())

			err = cli.Status().Update(ctx, dash)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(dash).Should(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, "foo", metav1.ConditionFalse),
			)

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: componentApi.DashboardInstanceName,
				},
			}

			cc := createReconciler(cli)
			cc.AddAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
				return tt.err
			})

			result, err := cc.Reconcile(ctx, req)
			if tt.err == nil {
				g.Expect(err).ShouldNot(HaveOccurred())
			} else {
				g.Expect(err).Should(MatchError(tt.err))
			}

			g.Expect(result.Requeue).Should(BeFalse())

			di := resources.GvkToUnstructured(gvk.Dashboard)
			di.SetName(dash.GetName())

			err = cli.Get(ctx, client.ObjectKeyFromObject(di), di)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(di).Should(tt.matcher)

			err = cli.Delete(ctx, di, client.PropagationPolicy(metav1.DeletePropagationBackground))
			g.Expect(err).ShouldNot(HaveOccurred())

			g.Eventually(func() ([]componentApi.Dashboard, error) {
				l := componentApi.DashboardList{}
				if err := cli.List(ctx, &l, client.InNamespace("")); err != nil {
					return nil, err
				}

				return l.Items, nil
			}).WithTimeout(10 * time.Second).Should(BeEmpty())
		})
	}
}

func TestReconcilerBuilderClientCaching(t *testing.T) {
	t.Parallel()

	// Create a test scheme and register required types
	s := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
	utilruntime.Must(dscv1.AddToScheme(s))
	utilruntime.Must(dsciv1.AddToScheme(s))

	// Create a hermetic manager (no real cluster) with registered scheme
	cfg := &rest.Config{Host: testHostURL} // not contacted; manager isn't started
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: s, Metrics: server.Options{BindAddress: "0"}})
	require.NoError(t, err)

	// Create a test object
	obj := &componentApi.Dashboard{}

	// Create ReconcilerBuilder
	builder := ReconcilerFor(mgr, obj)

	// First call to validateManager - this should initialize the cached clients
	err = builder.validateManager()
	require.NoError(t, err)

	// Capture the pointers to the cached clients after first initialization
	firstDiscoveryClient := builder.discoveryClient
	firstDynamicClient := builder.dynamicClient

	// Verify clients were initialized
	require.NotNil(t, firstDiscoveryClient, "Discovery client should be initialized")
	require.NotNil(t, firstDynamicClient, "Dynamic client should be initialized")

	// Second call to validateManager - this should reuse the cached clients
	err = builder.validateManager()
	require.NoError(t, err)

	// Capture the pointers to the cached clients after second initialization
	secondDiscoveryClient := builder.discoveryClient
	secondDynamicClient := builder.dynamicClient

	// Assert that the same client instances are reused (same pointers)
	require.Same(t, firstDiscoveryClient, secondDiscoveryClient,
		"Discovery client should be reused, not recreated")
	require.Same(t, firstDynamicClient, secondDynamicClient,
		"Dynamic client should be reused, not recreated")

	// Additional verification: ensure the clients are still accessible
	require.NotNil(t, firstDiscoveryClient, "Discovery client should be accessible")
	require.NotNil(t, firstDynamicClient, "Dynamic client should be accessible")
}

func TestReconcilerBuilderValidateManagerErrorPaths(t *testing.T) {
	t.Parallel()

	// Create a test scheme and register required types
	s := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
	utilruntime.Must(dscv1.AddToScheme(s))
	utilruntime.Must(dsciv1.AddToScheme(s))

	// Create a test object
	obj := &componentApi.Dashboard{}

	t.Run("validateManager handles nil config gracefully", func(t *testing.T) {
		t.Parallel()

		// Create a mock manager that returns nil config to simulate invalid manager state
		mockMgr := &mockManager{
			scheme: s,
			config: nil, // Invalid config
		}

		// Create ReconcilerBuilder
		builder := ReconcilerFor(mockMgr, obj)

		// Call validateManager - should panic due to nil config
		// This panic is expected as a defensive guard against misconfigured managers,
		// ensuring validateManager fails fast when the manager config is invalid
		require.Panics(t, func() {
			_ = builder.validateManager()
		}, "validateManager should panic when config is nil")

		// Verify no clients are cached after panic
		require.Nil(t, builder.discoveryClient, "Discovery client should not be cached after panic")
		require.Nil(t, builder.dynamicClient, "Dynamic client should not be cached after panic")
	})

	t.Run("validateManager succeeds with valid manager configuration", func(t *testing.T) {
		t.Parallel()

		// Create a manager with valid config
		validConfig := &rest.Config{
			Host: testHostURL, // Valid host but won't be contacted
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
		mgr, err := ctrl.NewManager(validConfig, ctrl.Options{Scheme: s, Metrics: server.Options{BindAddress: "0"}})
		require.NoError(t, err)

		// Create ReconcilerBuilder
		builder := ReconcilerFor(mgr, obj)

		// Call validateManager - should succeed since both clients can be created
		err = builder.validateManager()
		require.NoError(t, err, "validateManager should succeed when both clients can be created")
		require.NotNil(t, builder.discoveryClient, errDiscoveryClientCached)
		require.NotNil(t, builder.dynamicClient, errDynamicClientCached)
	})

	t.Run("validateManager handles client creation failures gracefully", func(t *testing.T) {
		t.Parallel()

		// Create a mock manager that returns a config that will cause client creation to fail
		mockMgr := &mockManager{
			scheme: s,
			config: &rest.Config{
				Host: "https://invalid-host-that-does-not-exist:6443",
				TLSClientConfig: rest.TLSClientConfig{
					Insecure: true,
				},
				Timeout: fastFailTimeout, // Fast timeout to force failure
			},
		}

		// Create ReconcilerBuilder
		builder := ReconcilerFor(mockMgr, obj)

		// Call validateManager - should succeed since client creation functions don't actually connect
		err := builder.validateManager()
		require.NoError(t, err, "validateManager should succeed even with invalid config since client creation doesn't connect")
		require.NotNil(t, builder.discoveryClient, errDiscoveryClientCached)
		require.NotNil(t, builder.dynamicClient, errDynamicClientCached)

		// Second call should reuse cached clients
		err = builder.validateManager()
		require.NoError(t, err, "validateManager should succeed and reuse cached clients")
	})

	t.Run("validateManager handles concurrent access safely", func(t *testing.T) {
		t.Parallel()

		// Create a manager with valid config
		validConfig := &rest.Config{
			Host: testHostURL, // Valid host but won't be contacted
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
		mgr, err := ctrl.NewManager(validConfig, ctrl.Options{Scheme: s, Metrics: server.Options{BindAddress: "0"}})
		require.NoError(t, err)

		// Create ReconcilerBuilder
		builder := ReconcilerFor(mgr, obj)

		// Test concurrent access to validateManager
		const numGoroutines = 10
		errs := make(chan error, numGoroutines)

		for range numGoroutines {
			go func() {
				errs <- builder.validateManager()
			}()
		}

		// Collect all results with timeout protection
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		resultsReceived := 0
		for resultsReceived < numGoroutines {
			select {
			case err := <-errs:
				require.NoError(t, err, "validateManager should succeed under concurrent access")
				resultsReceived++
			case <-ctx.Done():
				t.Fatal("Timeout waiting for concurrent validateManager calls")
			}
		}

		// Verify clients are cached
		require.NotNil(t, builder.discoveryClient, errDiscoveryClientCached+" after concurrent access")
		require.NotNil(t, builder.dynamicClient, errDynamicClientCached+" after concurrent access")
	})

	t.Run("validateManager succeeds with different manager configurations", func(t *testing.T) {
		t.Parallel()

		// Test with first manager configuration
		config1 := &rest.Config{
			Host: testHostURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
		mgr1, err := ctrl.NewManager(config1, ctrl.Options{Scheme: s, Metrics: server.Options{BindAddress: "0"}})
		require.NoError(t, err)

		// Create ReconcilerBuilder with first manager
		builder1 := ReconcilerFor(mgr1, obj)

		// First call should succeed
		err = builder1.validateManager()
		require.NoError(t, err, "validateManager should succeed with first manager")
		require.NotNil(t, builder1.discoveryClient, errDiscoveryClientCached)
		require.NotNil(t, builder1.dynamicClient, errDynamicClientCached)

		// Test with second manager configuration
		config2 := &rest.Config{
			Host: "https://127.0.0.1:2",
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
		mgr2, err := ctrl.NewManager(config2, ctrl.Options{Scheme: s, Metrics: server.Options{BindAddress: "0"}})
		require.NoError(t, err)

		// Create ReconcilerBuilder with second manager
		builder2 := ReconcilerFor(mgr2, obj)

		// Second call should succeed
		err = builder2.validateManager()
		require.NoError(t, err, "validateManager should succeed with second manager")
		require.NotNil(t, builder2.discoveryClient, errDiscoveryClientCached)
		require.NotNil(t, builder2.dynamicClient, errDynamicClientCached)

		// Verify that different managers create different client instances
		require.NotSame(t, builder1.discoveryClient, builder2.discoveryClient, "Different managers should create different discovery clients")
		require.NotSame(t, builder1.dynamicClient, builder2.dynamicClient, "Different managers should create different dynamic clients")
	})
}

func TestNewReconcilerErrorOrdering(t *testing.T) {
	t.Parallel()

	// Create a test scheme and register required types
	s := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))

	// Create a test object
	obj := &componentApi.Dashboard{}

	t.Run("empty name error comes before nil manager error", func(t *testing.T) {
		t.Parallel()

		// Test with both empty name and nil manager - should get empty name error first
		_, err := newReconcilerWithClients(
			nil, // nil manager
			"",  // empty name
			obj,
			nil, // nil discovery client
			nil, // nil dynamic client
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), errNameCannotBeEmpty)
		require.NotContains(t, err.Error(), errManagerCannotBeNil)
	})

	t.Run("manager nil error comes after name validation", func(t *testing.T) {
		t.Parallel()

		// Test with valid name but nil manager - should get manager nil error
		_, err := newReconcilerWithClients(
			nil,    // nil manager
			"test", // valid name
			obj,
			nil, // nil discovery client
			nil, // nil dynamic client
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), errManagerCannotBeNil)
		require.Contains(t, err.Error(), "test") // name should be included in error message
	})

	t.Run("whitespace-only name is treated as empty", func(t *testing.T) {
		t.Parallel()

		// Test with whitespace-only name and nil manager - should get empty name error first
		_, err := newReconcilerWithClients(
			nil,       // nil manager
			"   \t\n", // whitespace-only name
			obj,
			nil, // nil discovery client
			nil, // nil dynamic client
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), errNameCannotBeEmpty)
		require.NotContains(t, err.Error(), errManagerCannotBeNil)
	})
}

// mockClient is a test mock that implements the client.Client interface.
type mockClient struct {
	scheme *runtime.Scheme
}

func (c *mockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	// Return a valid GVK for the test object
	return schema.GroupVersionKind{
		Group:   "components.platform.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "Dashboard",
	}, nil
}

func (c *mockClient) Scheme() *runtime.Scheme {
	return c.scheme
}

// Implement other required methods with panics.
func (c *mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	panic(mockGetNotImplemented)
}

func (c *mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	panic(mockListNotImplemented)
}

func (c *mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	panic(mockCreateNotImplemented)
}

func (c *mockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	panic(mockDeleteNotImplemented)
}

func (c *mockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	panic(mockUpdateNotImplemented)
}

func (c *mockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	panic(mockPatchNotImplemented)
}

func (c *mockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	panic(mockDeleteAllOfNotImplemented)
}

func (c *mockClient) Status() client.StatusWriter {
	panic(mockStatusNotImplemented)
}

func (c *mockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	panic(mockIsObjectNamespacedNotImplemented)
}

func (c *mockClient) RESTMapper() meta.RESTMapper {
	panic(mockRESTMapperNotImplemented)
}

func (c *mockClient) SubResource(subResource string) client.SubResourceClient {
	panic(mockSubResourceNotImplemented)
}

// mockManager is a test mock that implements the manager.Manager interface
// to simulate various failure scenarios.
type mockManager struct {
	scheme *runtime.Scheme
	config *rest.Config
	client *mockClient
}

func (m *mockManager) GetConfig() *rest.Config {
	return m.config
}

func (m *mockManager) GetScheme() *runtime.Scheme {
	return m.scheme
}

func (m *mockManager) GetClient() client.Client {
	if m.client == nil {
		m.client = &mockClient{scheme: m.scheme}
	}
	return m.client
}

func (m *mockManager) GetFieldIndexer() client.FieldIndexer {
	panic(mockGetFieldIndexerNotImplemented)
}

func (m *mockManager) GetCache() cache.Cache {
	panic(mockGetCacheNotImplemented)
}

func (m *mockManager) GetEventRecorderFor(name string) record.EventRecorder {
	panic(mockGetEventRecorderForNotImplemented)
}

func (m *mockManager) GetRESTMapper() meta.RESTMapper {
	panic(mockGetRESTMapperNotImplemented)
}

func (m *mockManager) GetAPIReader() client.Reader {
	panic(mockGetAPIReaderNotImplemented)
}

func (m *mockManager) GetWebhookServer() webhook.Server {
	panic(mockGetWebhookServerNotImplemented)
}

func (m *mockManager) GetLogger() logr.Logger {
	panic(mockGetLoggerNotImplemented)
}

func (m *mockManager) GetControllerOptions() config.Controller {
	panic(mockGetControllerOptionsNotImplemented)
}

func (m *mockManager) Add(runnable manager.Runnable) error {
	panic(mockAddNotImplemented)
}

func (m *mockManager) ElectLeader() error {
	panic(mockElectLeaderNotImplemented)
}

func (m *mockManager) Elected() <-chan struct{} {
	panic(mockElectedNotImplemented)
}

func (m *mockManager) GetHTTPClient() *http.Client {
	panic(mockGetHTTPClientNotImplemented)
}

func (m *mockManager) AddMetricsExtraHandler(path string, handler http.Handler) error {
	panic(mockAddMetricsExtraHandlerNotImplemented)
}

func (m *mockManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	panic(mockAddMetricsServerExtraHandlerNotImplemented)
}

func (m *mockManager) AddHealthzCheck(name string, check healthz.Checker) error {
	panic(mockAddHealthzCheckNotImplemented)
}

func (m *mockManager) AddReadyzCheck(name string, check healthz.Checker) error {
	panic(mockAddReadyzCheckNotImplemented)
}

func (m *mockManager) Start(ctx context.Context) error {
	panic(mockStartNotImplemented)
}
