package hardwareprofile_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// WithQueueBasedScheduling returns a functional option that sets the SchedulingType field to QueueScheduling.
func WithQueueBasedSchedulingType() func(*hwpv1alpha1.HardwareProfile) {
	return func(hwp *hwpv1alpha1.HardwareProfile) {
		if hwp.Spec.SchedulingSpec == nil {
			hwp.Spec.SchedulingSpec = &hwpv1alpha1.SchedulingSpec{}
		}
		hwp.Spec.SchedulingSpec.SchedulingType = hwpv1alpha1.QueueScheduling
	}
}

// WithNodeBasedScheduling returns a functional option that sets the SchedulingType to NodeScheduling.
func WithNodeBasedSchedulingType() func(*hwpv1alpha1.HardwareProfile) {
	return func(hwp *hwpv1alpha1.HardwareProfile) {
		if hwp.Spec.SchedulingSpec == nil {
			hwp.Spec.SchedulingSpec = &hwpv1alpha1.SchedulingSpec{}
		}
		hwp.Spec.SchedulingSpec.SchedulingType = hwpv1alpha1.NodeScheduling
	}
}

// WithQueueBasedSchedulingConfig returns a functional option that sets the KueueSchedulingSpec configuration.
func WithQueueBasedSchedulingConfig() func(*hwpv1alpha1.HardwareProfile) {
	return func(hwp *hwpv1alpha1.HardwareProfile) {
		if hwp.Spec.SchedulingSpec.Kueue == nil {
			hwp.Spec.SchedulingSpec.Kueue = &hwpv1alpha1.KueueSchedulingSpec{}
		}
		hwp.Spec.SchedulingSpec.Kueue.LocalQueueName = "test-queue"
	}
}

// WithNodeBasedSchedulingConfig returns a functional option that sets the NodeSchedulingSpec configuration.
func WithNodeBasedSchedulingConfig() func(*hwpv1alpha1.HardwareProfile) {
	return func(hwp *hwpv1alpha1.HardwareProfile) {
		if hwp.Spec.SchedulingSpec.Node == nil {
			hwp.Spec.SchedulingSpec.Node = &hwpv1alpha1.NodeSchedulingSpec{}
		}
		hwp.Spec.SchedulingSpec.Node.NodeSelector = map[string]string{
			"test": "node",
		}
	}
}

// TestHardwareProfile_Integration exercises the validating logic for HardwareProfile resources.
// It verifies the union discriminating CEL rules for queue vs node based scheduling when creating HardwareProfiles.
func TestHardwareProfile_Integration(t *testing.T) {
	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "Allows creation of queue-based HardwareProfile when NodeSchedulingSpec is nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				hwp := envtestutil.NewHWP(
					"valid-queue-based-hwp",
					ns,
					WithQueueBasedSchedulingType(),
					WithQueueBasedSchedulingConfig(),
				)
				g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed(), "should allow creation of a queue-based HardwareProfile when NodeSchedulingSpec is nil")
			},
		},
		{
			name: "Denies creation of queue-based HardwareProfile when NodeSchedulingSpec is not nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				hwp := envtestutil.NewHWP(
					"invalid-queue-based-hwp",
					ns,
					WithQueueBasedSchedulingType(),
					WithQueueBasedSchedulingConfig(),
					WithNodeBasedSchedulingConfig(),
				)
				err := k8sClient.Create(ctx, hwp)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a queue-based HardwareProfile when NodeSchedulingSpec is not nil")
			},
		},
		{
			name: "Allows creation of node-based HardwareProfile when KueueSchedulingSpec is nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				hwp := envtestutil.NewHWP(
					"valid-node-based-hwp",
					ns,
					WithNodeBasedSchedulingType(),
					WithNodeBasedSchedulingConfig(),
				)
				g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed(), "should allow creation of a node-based HardwareProfile when KueueSchedulingSpec is nil")
			},
		},
		{
			name: "Denies creation of node-based HardwareProfile when KueueSchedulingSpec is not nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				hwp := envtestutil.NewHWP(
					"invalid-node-based-hwp",
					ns,
					WithNodeBasedSchedulingType(),
					WithNodeBasedSchedulingConfig(),
					WithQueueBasedSchedulingConfig(),
				)
				err := k8sClient.Create(ctx, hwp)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a node-based HardwareProfile when KueueSchedulingSpec is not nil")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Starting test case: %s", tc.name)

			g := NewWithT(t)
			gctx, cancel := context.WithCancel(context.Background())

			s, err := scheme.New()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(hwpv1alpha1.AddToScheme(s)).To(Succeed())

			env, err := envt.New(envt.WithManager(), envt.WithScheme(s))
			g.Expect(err).ShouldNot(HaveOccurred())

			t.Cleanup(func() {
				cancel()

				err := env.Stop()
				g.Expect(err).NotTo(HaveOccurred())
			})

			ns := corev1.Namespace{}
			ns.Name = xid.New().String()
			err = env.Client().Create(gctx, &ns)
			g.Expect(err).ShouldNot(HaveOccurred())
			t.Logf("Using namespace: %s", ns.Name)

			tc.test(g, gctx, env.Client(), ns.Name)
			t.Logf("Finished test case: %s", tc.name)
		})
	}
}
