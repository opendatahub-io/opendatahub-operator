package dscinitialization_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr/funcr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	dscictrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

type noopRecorder struct{}

func (noopRecorder) Eventf(_ runtime.Object, _ runtime.Object, _, _, _, _ string, _ ...interface{}) {}

func TestLogErrorsStructuredFields(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, _ ...client.ListOption) error {
				if _, ok := list.(*dsciv2.DSCInitializationList); ok {
					return errors.New("injected list error")
				}
				return nil
			},
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())

	reconciler := &dscictrl.DSCInitializationReconciler{
		Client:   cli,
		Recorder: &noopRecorder{},
	}

	var logged []string
	logger := funcr.New(func(_, args string) {
		logged = append(logged, args)
	}, funcr.Options{})
	ctx := logf.IntoContext(t.Context(), logger)

	_, err = reconciler.Reconcile(ctx, ctrl.Request{})

	g.Expect(err).To(HaveOccurred())
	g.Expect(logged).To(ContainElement(And(
		ContainSubstring(`"msg"="Failed to retrieve DSCInitialization resource."`),
		ContainSubstring(`"resourceKind"="DSCInitialization"`),
		ContainSubstring(`"name"=""`),
		Not(ContainSubstring(`"Request.Name"`)),
	)))
}
