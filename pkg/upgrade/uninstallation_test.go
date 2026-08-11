package upgrade_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestUninstallDeletesDSCBeforeDSCI(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	var dscDeletedBeforeDSCI atomic.Bool
	dscDeletedBeforeDSCI.Store(true)
	var dscDeleted atomic.Bool

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsc, dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
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
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	uninstallErr := upgrade.OperatorUninstall(ctx, cli, "")

	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSC"))
	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSCI"))

	g.Expect(dscDeletedBeforeDSCI.Load()).To(BeTrue(),
		"DSC should be deleted before DSCI deletion is attempted")

	var dscList dscv2.DataScienceClusterList
	g.Expect(cli.List(ctx, &dscList)).To(Succeed())
	g.Expect(dscList.Items).To(BeEmpty(), "all DSC objects should be deleted")

	var dsciList dsciv2.DSCInitializationList
	g.Expect(cli.List(ctx, &dsciList)).To(Succeed())
	g.Expect(dsciList.Items).To(BeEmpty(), "all DSCI objects should be deleted")
}

func TestUninstallWaitsForDSCRemoval(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	var listCallCount atomic.Int32
	var deleteAllOfCalled atomic.Bool
	var dscRemovedBeforeDSCIDelete atomic.Bool
	dscRemovedBeforeDSCIDelete.Store(true)
	var dscActuallyRemoved atomic.Bool

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsc, dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*dscv2.DataScienceCluster); ok {
					deleteAllOfCalled.Store(true)
					return nil
				}
				if _, ok := obj.(*dsciv2.DSCInitialization); ok {
					if !dscActuallyRemoved.Load() {
						dscRemovedBeforeDSCIDelete.Store(false)
					}
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
			List: func(ctx2 context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*dscv2.DataScienceClusterList); ok && deleteAllOfCalled.Load() {
					count := listCallCount.Add(1)
					if count >= 2 {
						_ = c.DeleteAllOf(ctx2, &dscv2.DataScienceCluster{})
						dscActuallyRemoved.Store(true)
					}
				}
				return c.List(ctx2, list, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	uninstallErr := upgrade.OperatorUninstall(ctx, cli, "")

	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSC"))
	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSCI"))

	g.Expect(listCallCount.Load()).To(BeNumerically(">=", 2),
		"removeDSC should have polled multiple times waiting for DSC deletion")
	g.Expect(dscRemovedBeforeDSCIDelete.Load()).To(BeTrue(),
		"DSCI deletion should only occur after DSC objects are fully removed")

	var dscList dscv2.DataScienceClusterList
	g.Expect(cli.List(ctx, &dscList)).To(Succeed())
	g.Expect(dscList.Items).To(BeEmpty(), "all DSC objects should be deleted after waiting")
}

func TestUninstallSucceedsWithNoDSC(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	var dscDeleteAllOfCalled atomic.Bool
	var dsciDeleteAllOfCalled atomic.Bool

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				switch obj.(type) {
				case *dscv2.DataScienceCluster:
					dscDeleteAllOfCalled.Store(true)
				case *dsciv2.DSCInitialization:
					dsciDeleteAllOfCalled.Store(true)
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	uninstallErr := upgrade.OperatorUninstall(ctx, cli, "")

	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSC"))
	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSCI"))

	g.Expect(dscDeleteAllOfCalled.Load()).To(BeTrue(),
		"removeDSC should still be called even with no DSC objects")
	g.Expect(dsciDeleteAllOfCalled.Load()).To(BeTrue(),
		"removeDSCI should proceed after removeDSC succeeds")

	var dsciList dsciv2.DSCInitializationList
	g.Expect(cli.List(ctx, &dsciList)).To(Succeed())
	g.Expect(dsciList.Items).To(BeEmpty(), "DSCI should be deleted")
}

func TestUninstallPropagatesDSCDeleteError(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	var dsciDeleteCalled atomic.Bool

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*dscv2.DataScienceCluster); ok {
					return errors.New("simulated DSC deletion failure")
				}
				if _, ok := obj.(*dsciv2.DSCInitialization); ok {
					dsciDeleteCalled.Store(true)
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	uninstallErr := upgrade.OperatorUninstall(ctx, cli, "")

	g.Expect(uninstallErr).To(HaveOccurred())
	g.Expect(uninstallErr.Error()).To(ContainSubstring("failure deleting DSC"))
	g.Expect(dsciDeleteCalled.Load()).To(BeFalse(),
		"DSCI deletion should not be attempted when DSC deletion fails")
}

func TestUninstallPropagatesListErrorDuringPolling(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	var dsciDeleteCalled atomic.Bool
	var deleteAllOfCalled atomic.Bool

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsc, dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*dscv2.DataScienceCluster); ok {
					deleteAllOfCalled.Store(true)
					return nil
				}
				if _, ok := obj.(*dsciv2.DSCInitialization); ok {
					dsciDeleteCalled.Store(true)
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
			List: func(ctx2 context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*dscv2.DataScienceClusterList); ok && deleteAllOfCalled.Load() {
					return errors.New("simulated List failure")
				}
				return c.List(ctx2, list, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	uninstallErr := upgrade.OperatorUninstall(ctx, cli, "")

	g.Expect(uninstallErr).To(HaveOccurred())
	g.Expect(uninstallErr.Error()).To(ContainSubstring("failure waiting for DSC deletion"))
	g.Expect(dsciDeleteCalled.Load()).To(BeFalse(),
		"DSCI deletion should not be attempted when List fails during polling")
}

func TestUninstallHandlesMultipleDSCs(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsc1 := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "dsc-one"},
	}
	dsc2 := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "dsc-two"},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	var dsciDeletedAfterAllDSC atomic.Bool
	dsciDeletedAfterAllDSC.Store(true)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsc1, dsc2, dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*dsciv2.DSCInitialization); ok {
					dscList := &dscv2.DataScienceClusterList{}
					if err := c.List(ctx2, dscList); err != nil {
						return err
					}
					if len(dscList.Items) > 0 {
						dsciDeletedAfterAllDSC.Store(false)
					}
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	uninstallErr := upgrade.OperatorUninstall(ctx, cli, "")

	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSC"))
	g.Expect(uninstallErr.Error()).ToNot(ContainSubstring("failure deleting DSCI"))

	g.Expect(dsciDeletedAfterAllDSC.Load()).To(BeTrue(),
		"DSCI deletion should only occur after all DSC objects are removed")

	var dscList dscv2.DataScienceClusterList
	g.Expect(cli.List(ctx, &dscList)).To(Succeed())
	g.Expect(dscList.Items).To(BeEmpty(), "all DSC objects should be deleted")
}

func TestUninstallTimeoutIncludesRemainingDSCNames(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck-dsc"},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
	}

	var dsciDeleteCalled atomic.Bool

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsc, dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			DeleteAllOf: func(ctx2 context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*dscv2.DataScienceCluster); ok {
					return nil
				}
				if _, ok := obj.(*dsciv2.DSCInitialization); ok {
					dsciDeleteCalled.Store(true)
				}
				return c.DeleteAllOf(ctx2, obj, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	uninstallErr := upgrade.OperatorUninstall(timeoutCtx, cli, "")

	g.Expect(uninstallErr).To(HaveOccurred())
	g.Expect(uninstallErr.Error()).To(ContainSubstring("stuck-dsc"))
	g.Expect(uninstallErr.Error()).To(ContainSubstring("failure waiting for DSC deletion"))
	g.Expect(dsciDeleteCalled.Load()).To(BeFalse(),
		"DSCI deletion should not be attempted when DSC deletion times out")
}
