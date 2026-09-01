//nolint:testpackage
package dscinitialization

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr/funcr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

type noopRecorder struct{}

func (noopRecorder) Eventf(_ runtime.Object, _ runtime.Object, _, _, _, _ string, _ ...any) {}

func logCapturingContext(t *testing.T) (context.Context, *[]string) {
	t.Helper()
	var logged []string
	logger := funcr.New(func(_, args string) {
		logged = append(logged, args)
	}, funcr.Options{})
	return logf.IntoContext(t.Context(), logger), &logged
}

// TestReconcileLogsStructuredFinalizerField reconciles a real DSCInitialization
// that has no finalizer yet, and asserts that the "Adding finalizer" log line
// carries the camelCase "finalizer" key populated with the actual finalizer
// value. This exercises a real reconcile path and asserts on a real, non-empty
// structured field rather than the absence of fields.
func TestReconcileLogsStructuredFinalizerField(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	s, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	cli, err := fakeclient.New(
		fakeclient.WithScheme(s),
		fakeclient.WithObjects(dsci),
	)
	g.Expect(err).NotTo(HaveOccurred())

	reconciler := &DSCInitializationReconciler{
		Client:   cli,
		Scheme:   s,
		Recorder: &noopRecorder{},
	}

	ctx, logged := logCapturingContext(t)

	// Reconcile may return an error from later stages (e.g. creating operator
	// resources), but the finalizer log is emitted early and unconditionally
	// once the finalizer is found to be missing.
	_, _ = reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: dsci.Name},
	})

	g.Expect(*logged).To(ContainElement(And(
		ContainSubstring(`"msg"="Adding finalizer for DSCInitialization"`),
		ContainSubstring(`"finalizer"="`+finalizerName+`"`),
	)), "expected the finalizer log to carry a populated camelCase finalizer field")

	// The finalizer must actually be persisted on the instance.
	updated := &dsciv2.DSCInitialization{}
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: dsci.Name}, updated)).To(Succeed())
	g.Expect(updated.Finalizers).To(ContainElement(finalizerName))
}

// TestWatchHandlersLogResourceKindOnListError verifies that when a watch
// handler fails to List its target resource, it logs the failure with the
// camelCase "resourceKind" key set to the *watched* kind (not a hard-coded
// "DSCInitialization"). Each handler List's a different type, so the fake
// client's List interceptor can fail unconditionally.
func TestWatchHandlersLogResourceKindOnListError(t *testing.T) {
	listErr := errors.New("injected list error")

	tests := []struct {
		name             string
		call             func(r *DSCInitializationReconciler, ctx context.Context) []reconcile.Request
		wantMsg          string
		wantResourceKind string
	}{
		{
			name: "watchAuthResource",
			call: func(r *DSCInitializationReconciler, ctx context.Context) []reconcile.Request {
				return r.watchAuthResource(ctx, &metav1.PartialObjectMetadata{})
			},
			wantMsg:          "Failed to get AuthList",
			wantResourceKind: "Auth",
		},
		{
			name: "watchGatewayConfigResource",
			call: func(r *DSCInitializationReconciler, ctx context.Context) []reconcile.Request {
				return r.watchGatewayConfigResource(ctx, &metav1.PartialObjectMetadata{})
			},
			wantMsg:          "Failed to get GatewayConfigList",
			wantResourceKind: "GatewayConfig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			cli, err := fakeclient.New(
				fakeclient.WithInterceptorFuncs(interceptor.Funcs{
					List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
						return listErr
					},
				}),
			)
			g.Expect(err).NotTo(HaveOccurred())

			reconciler := &DSCInitializationReconciler{Client: cli}

			ctx, logged := logCapturingContext(t)

			// The handlers swallow the List error and return a (possibly nil)
			// request slice; the observable contract is the structured log.
			_ = tt.call(reconciler, ctx)

			g.Expect(*logged).To(ContainElement(And(
				ContainSubstring(`"msg"="`+tt.wantMsg+`"`),
				ContainSubstring(`"resourceKind"="`+tt.wantResourceKind+`"`),
			)), "expected a structured error log with camelCase resourceKind=%q", tt.wantResourceKind)
		})
	}
}
