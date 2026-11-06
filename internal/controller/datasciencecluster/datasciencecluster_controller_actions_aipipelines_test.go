//nolint:testpackage
package datasciencecluster

import (
	"context"
	"errors"
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func newLegacyDataSciencePipelines() *unstructured.Unstructured {
	legacy := &unstructured.Unstructured{}
	legacy.SetGroupVersionKind(legacyDataSciencePipelinesGVK)
	legacy.SetName("default-datasciencepipelines")
	return legacy
}

func TestCleanupLegacyDataSciencePipelines(t *testing.T) {
	g := NewWithT(t)
	legacy := newLegacyDataSciencePipelines()
	legacy.SetFinalizers([]string{"datasciencepipelines.components.platform.opendatahub.io/finalizer"})
	other := newLegacyDataSciencePipelines()
	other.SetName("other-datasciencepipelines")

	cli, err := fakeclient.New(
		fakeclient.WithObjects(legacy, other),
		fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: legacyDataSciencePipelinesGVK, Scope: meta.RESTScopeRoot}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtype.ReconciliationRequest{Client: cli}

	err = cleanupLegacyDataSciencePipelines(t.Context(), rr)
	var stopErr odherrors.StopError
	g.Expect(errors.As(err, &stopErr)).To(BeTrue())
	g.Expect(stopErr.RequeueAfter()).To(BeNumerically(">", 0))

	got := newLegacyDataSciencePipelines()
	err = cli.Get(t.Context(), client.ObjectKeyFromObject(got), got)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(other), other)).To(Succeed())
	g.Expect(cleanupLegacyDataSciencePipelines(t.Context(), rr)).To(Succeed())
}

func TestCleanupLegacyDataSciencePipelinesWithoutCRD(t *testing.T) {
	g := NewWithT(t)
	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return &meta.NoKindMatchError{GroupKind: legacyDataSciencePipelinesGVK.GroupKind(), SearchedVersions: []string{"v1alpha1"}}
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cleanupLegacyDataSciencePipelines(t.Context(), &odhtype.ReconciliationRequest{Client: cli})).To(Succeed())
}

func TestCleanupLegacyDataSciencePipelinesErrors(t *testing.T) {
	for _, operation := range []string{"get", "patch", "delete"} {
		t.Run(operation, func(t *testing.T) {
			g := NewWithT(t)
			legacy := newLegacyDataSciencePipelines()
			legacy.SetFinalizers([]string{"stale-finalizer"})
			failure := errors.New("API unavailable")
			interceptors := interceptor.Funcs{
				Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if operation == "get" {
						return failure
					}
					return cli.Get(ctx, key, obj, opts...)
				},
				Patch: func(ctx context.Context, cli client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if operation == "patch" {
						return failure
					}
					return cli.Patch(ctx, obj, patch, opts...)
				},
				Delete: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if operation == "delete" {
						return failure
					}
					return cli.Delete(ctx, obj, opts...)
				},
			}
			cli, err := fakeclient.New(
				fakeclient.WithObjects(legacy),
				fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: legacyDataSciencePipelinesGVK, Scope: meta.RESTScopeRoot}),
				fakeclient.WithInterceptorFuncs(interceptors),
			)
			g.Expect(err).NotTo(HaveOccurred())
			err = cleanupLegacyDataSciencePipelines(t.Context(), &odhtype.ReconciliationRequest{Client: cli})
			g.Expect(errors.Is(err, failure)).To(BeTrue())
		})
	}
}

func TestCleanupLegacyDataSciencePipelinesAlreadyDeleting(t *testing.T) {
	g := NewWithT(t)
	legacy := newLegacyDataSciencePipelines()
	legacy.SetFinalizers([]string{"stale-finalizer"})
	cli, err := fakeclient.New(
		fakeclient.WithObjects(legacy),
		fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: legacyDataSciencePipelinesGVK, Scope: meta.RESTScopeRoot}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cli.Delete(t.Context(), legacy)).To(Succeed())

	rr := &odhtype.ReconciliationRequest{Client: cli}
	err = cleanupLegacyDataSciencePipelines(t.Context(), rr)
	var stopErr odherrors.StopError
	g.Expect(errors.As(err, &stopErr)).To(BeTrue())
	g.Expect(cleanupLegacyDataSciencePipelines(t.Context(), rr)).To(Succeed())
}
