package upgrade_test

import (
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestOperatorUninstallDeletesDSCBeforeDSCI(t *testing.T) {
	ctx := t.Context()

	t.Run("should delete DSC objects before attempting DSCI deletion", func(t *testing.T) {
		g := NewWithT(t)

		dsc := &dscv2.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-dsc",
			},
		}

		dsci := &dsciv2.DSCInitialization{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-dsci",
			},
		}

		var dscDeletedBeforeDSCI atomic.Bool
		dscDeletedBeforeDSCI.Store(true)
		var dscDeleted atomic.Bool

		interceptorFuncs := interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				switch obj.(type) {
				case *dscv2.DataScienceCluster:
					if err := c.DeleteAllOf(ctx2, obj, opts...); err != nil {
						return err
					}
					dscDeleted.Store(true)
					return nil
				case *dsciv2.DSCInitialization:
					if !dscDeleted.Load() {
						dscDeletedBeforeDSCI.Store(false)
					}
					return c.DeleteAllOf(ctx2, obj, opts...)
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsc, dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// OperatorUninstall will fail later (on GetOperatorNamespace) but DSC/DSCI
		// deletion ordering happens first. We only care about the ordering.
		_ = upgrade.OperatorUninstall(ctx, cli, "")

		g.Expect(dscDeletedBeforeDSCI.Load()).To(BeTrue(),
			"DSC should be deleted before DSCI deletion is attempted")

		var dscList dscv2.DataScienceClusterList
		g.Expect(cli.List(ctx, &dscList)).To(Succeed())
		g.Expect(dscList.Items).To(BeEmpty(), "all DSC objects should be deleted")

		var dsciList dsciv2.DSCInitializationList
		g.Expect(cli.List(ctx, &dsciList)).To(Succeed())
		g.Expect(dsciList.Items).To(BeEmpty(), "all DSCI objects should be deleted")
	})

	t.Run("should wait for DSC objects to be fully removed", func(t *testing.T) {
		g := NewWithT(t)

		dsc := &dscv2.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-dsc",
			},
		}

		dsci := &dsciv2.DSCInitialization{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-dsci",
			},
		}

		// Simulate delayed deletion: DeleteAllOf for DSC is a no-op (foreground
		// propagation delay), and the DSC is only actually removed on the second
		// List poll.
		var listCallCount atomic.Int32
		var deleteAllOfCalled atomic.Bool

		interceptorFuncs := interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*dscv2.DataScienceCluster); ok {
					deleteAllOfCalled.Store(true)
					return nil
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
			List: func(ctx2 context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*dscv2.DataScienceClusterList); ok && deleteAllOfCalled.Load() {
					count := listCallCount.Add(1)
					if count >= 2 {
						_ = c.DeleteAllOf(ctx2, &dscv2.DataScienceCluster{})
					}
				}
				return c.List(ctx2, list, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsc, dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		_ = upgrade.OperatorUninstall(ctx, cli, "")

		g.Expect(listCallCount.Load()).To(BeNumerically(">=", 2),
			"removeDSC should have polled multiple times waiting for DSC deletion")

		var dscList dscv2.DataScienceClusterList
		g.Expect(cli.List(ctx, &dscList)).To(Succeed())
		g.Expect(dscList.Items).To(BeEmpty(), "all DSC objects should be deleted after waiting")
	})
}
