//nolint:testpackage
package reconciler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	mockDashboardName = "mock-dashboard"
	mockDsciName      = "mock-dsci"
	finalizerName     = "platform.opendatahub.io/finalizer"
)

func mockFinalizerAction(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	return nil
}

type MockManager struct {
	client client.Client
	scheme *runtime.Scheme
	mapper meta.RESTMapper
}

//nolint:ireturn
func (f *MockManager) GetClient() client.Client   { return f.client }
func (f *MockManager) GetScheme() *runtime.Scheme { return f.scheme }

//nolint:ireturn
func (f *MockManager) GetRESTMapper() meta.RESTMapper { return f.mapper }
func (f *MockManager) GetConfig() *rest.Config        { return &rest.Config{} }

//nolint:ireturn
func (f *MockManager) GetFieldIndexer() client.FieldIndexer { return nil }

//nolint:ireturn
func (f *MockManager) GetEventRecorderFor(name string) record.EventRecorder { return nil }

//nolint:ireturn
func (f *MockManager) GetCache() cache.Cache                                    { return nil }
func (f *MockManager) GetLogger() logr.Logger                                   { return ctrl.Log }
func (f *MockManager) Add(runnable manager.Runnable) error                      { return nil }
func (f *MockManager) Elected() <-chan struct{}                                 { ch := make(chan struct{}); close(ch); return ch }
func (f *MockManager) Start(ctx context.Context) error                          { <-ctx.Done(); return nil }
func (f *MockManager) AddHealthzCheck(name string, check healthz.Checker) error { return nil }
func (f *MockManager) AddMetricsServerExtraHandler(name string, handler http.Handler) error {
	return nil
}
func (f *MockManager) AddReadyzCheck(name string, check healthz.Checker) error { return nil }

//nolint:ireturn
func (f *MockManager) GetAPIReader() client.Reader             { return nil }
func (f *MockManager) GetControllerOptions() config.Controller { return config.Controller{} }
func (f *MockManager) GetHTTPClient() *http.Client             { return &http.Client{} }

//nolint:ireturn
func (f *MockManager) GetWebhookServer() webhook.Server { return nil }

//nolint:ireturn
func setupTest(mockDashboard *componentApi.Dashboard) (context.Context, *MockManager, client.WithWatch) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = componentApi.AddToScheme(scheme)
	_ = dsciv1.AddToScheme(scheme)

	mockDsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: mockDsciName,
		},
		Spec: dsciv1.DSCInitializationSpec{},
	}

	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{componentApi.GroupVersion})
	mapper.Add(
		schema.GroupVersionKind{
			Group:   componentApi.GroupVersion.Group,
			Version: componentApi.GroupVersion.Version,
			Kind:    componentApi.DashboardKind,
		},
		meta.RESTScopeNamespace,
	)

	mockClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRESTMapper(mapper).
		WithObjects(mockDashboard, mockDsci).
		Build()

	mockMgr := &MockManager{client: mockClient, scheme: scheme, mapper: mapper}

	return ctx, mockMgr, mockClient
}

func TestFinalizer_Add(t *testing.T) {
	g := gomega.NewWithT(t)

	mockDashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name: mockDashboardName,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.DashboardKind,
			APIVersion: componentApi.GroupVersion.Version,
		},
	}

	ctx, mgr, cli := setupTest(mockDashboard)

	r, err := ReconcilerFor(mgr, mockDashboard).
		WithFinalizer(mockFinalizerAction).
		Build(ctx)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(r.Finalizer).To(gomega.HaveLen(1))

	d := &componentApi.Dashboard{}
	err = cli.Get(
		ctx,
		client.ObjectKey{
			Name: mockDashboardName,
		},
		d,
	)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(controllerutil.ContainsFinalizer(d, finalizerName)).To(gomega.BeFalse())

	_, err = r.Reconcile(ctx, reconcile.Request{
		NamespacedName: client.ObjectKey{
			Name: mockDashboardName,
		},
	})
	g.Expect(err).ToNot(gomega.HaveOccurred())

	d = &componentApi.Dashboard{}
	err = cli.Get(
		ctx,
		client.ObjectKey{
			Name: mockDashboardName,
		},
		d,
	)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(controllerutil.ContainsFinalizer(d, finalizerName)).To(gomega.BeTrue())
}

func TestFinalizer_NotPresent(t *testing.T) {
	g := gomega.NewWithT(t)

	mockDashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name: mockDashboardName,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.DashboardKind,
			APIVersion: componentApi.GroupVersion.Version,
		},
	}

	ctx, mgr, cli := setupTest(mockDashboard)

	r, err := ReconcilerFor(mgr, mockDashboard).Build(ctx)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(r.Finalizer).To(gomega.BeEmpty())

	_, err = r.Reconcile(ctx, reconcile.Request{
		NamespacedName: client.ObjectKey{
			Name: mockDashboardName,
		},
	})
	g.Expect(err).ToNot(gomega.HaveOccurred())

	d := &componentApi.Dashboard{}
	err = cli.Get(
		ctx,
		client.ObjectKey{
			Name: mockDashboardName,
		},
		d,
	)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(controllerutil.ContainsFinalizer(d, finalizerName)).To(gomega.BeFalse())
}

func TestFinalizer_Remove(t *testing.T) {
	g := gomega.NewWithT(t)

	mockDashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       mockDashboardName,
			Finalizers: []string{platformFinalizer},
			DeletionTimestamp: &metav1.Time{
				Time: time.Now(),
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.DashboardKind,
			APIVersion: componentApi.GroupVersion.Version,
		},
	}

	ctx, mgr, cli := setupTest(mockDashboard)

	r, err := ReconcilerFor(mgr, mockDashboard).
		WithFinalizer(mockFinalizerAction).
		Build(ctx)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(r.Finalizer).To(gomega.HaveLen(1))

	_, err = r.Reconcile(ctx, reconcile.Request{
		NamespacedName: client.ObjectKey{
			Name: mockDashboardName,
		},
	})
	g.Expect(err).ToNot(gomega.HaveOccurred())

	d := &componentApi.Dashboard{}
	err = cli.Get(
		ctx,
		client.ObjectKey{
			Name: mockDashboardName,
		},
		d,
	)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(client.IgnoreNotFound(err)).To(gomega.Succeed())
}
