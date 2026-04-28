package certmanager_test

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	ctypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestNewCleanupActionNoCRD verifies that the finalizer action is a no-op when the
// certmanagers.operator.openshift.io CRD is not registered on the cluster.
func TestNewCleanupActionNoCRD(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid-1234")

	rr := &ctypes.ReconciliationRequest{
		Client:   envTest.Client(),
		Instance: instance,
	}

	// CRD is NOT registered. Get returns a NoKindMatchError, which must be treated
	// as NotFound (nothing to clean up) rather than propagated as a failure.
	err = certmanager.NewCleanupAction()(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())
}

// TestNewCleanupAction verifies the finalizer action that deletes
// CertManager/cluster before cascade deletion is released.
func TestNewCleanupAction(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	_, err = envTest.RegisterCRD(ctx,
		gvk.CertManagerV1Alpha1,
		"certmanagers", "certmanager",
		apiextensionsv1.ClusterScoped,
	)
	g.Expect(err).NotTo(HaveOccurred())

	cli := envTest.Client()
	action := certmanager.NewCleanupAction()

	// instance represents the reconciled AKE/CWE CR. Only its UID is used by the action.
	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid-1234")

	rr := &ctypes.ReconciliationRequest{
		Client:   cli,
		Instance: instance,
	}

	t.Run("no-op when Terminating with only non-cert-manager-operator finalizers", func(t *testing.T) {
		g := NewWithT(t)

		// Simulate Kubernetes foreground deletion: DeletionTimestamp set and only the
		// internal "foregroundDeletion" finalizer remains. cert-manager-operator's own
		// finalizers are already gone, so the cleanup action should return nil.
		cm := certManagerCR([]metav1.OwnerReference{
			ownerRef(instance.GetUID()),
		}, []string{"foregroundDeletion"})
		g.Expect(cli.Create(ctx, cm)).To(Succeed())
		t.Cleanup(func() {
			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(gvk.CertManagerV1Alpha1)
			if err := cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err == nil {
				got.SetFinalizers(nil)
				_ = cli.Update(ctx, got)
				_ = cli.Delete(ctx, got)
			}
		})

		// Trigger Delete so DeletionTimestamp is set.
		g.Expect(cli.Delete(ctx, cm)).To(Succeed())

		err := action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("no-op when CertManager/cluster does not exist", func(t *testing.T) {
		g := NewWithT(t)

		err := action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("no-op when CertManager/cluster exists but OwnerRef UID does not match", func(t *testing.T) {
		g := NewWithT(t)

		cm := certManagerCR([]metav1.OwnerReference{
			ownerRef("some-other-uid-9999"),
		}, nil)
		g.Expect(cli.Create(ctx, cm)).To(Succeed())
		t.Cleanup(func() { _ = cli.Delete(ctx, cm) })

		err := action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
	})

	// This subtest covers the full owned-CR lifecycle in sequence:
	//   1. CR exists with matching UID → action deletes it, DeletionTimestamp set, error returned.
	//   2. CR still Terminating (finalizer held) → action skips Delete, error returned.
	//   3. Finalizer removed → CR gone → action returns nil.
	// All three phases are in one subtest to share the CR state without t.Cleanup races.
	t.Run("owned CertManager/cluster lifecycle: delete, wait, gone", func(t *testing.T) {
		g := NewWithT(t)

		// Create the CR owned by this instance with a finalizer matching the
		// cert-manager-operator prefix, simulating the operator's runtime finalizers.
		cm := certManagerCR([]metav1.OwnerReference{
			ownerRef(instance.GetUID()),
		}, []string{"cert-manager-operator.operator.openshift.io/test-hold"})
		g.Expect(cli.Create(ctx, cm)).To(Succeed())
		t.Cleanup(func() {
			// Ensure the CR is cleaned up even if the test fails mid-way.
			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(gvk.CertManagerV1Alpha1)
			if err := cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err == nil {
				got.SetFinalizers(nil)
				_ = cli.Update(ctx, got)
				_ = cli.Delete(ctx, got)
			}
		})

		// Phase 1: CR present, DeletionTimestamp not yet set → action triggers Delete.
		err := action(ctx, rr)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("waiting for CertManager/cluster"))

		// Verify Delete was triggered: CR must now have a DeletionTimestamp.
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(gvk.CertManagerV1Alpha1)
		g.Expect(cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		g.Expect(got.GetDeletionTimestamp()).NotTo(BeNil())

		// Phase 2: CR is Terminating (finalizer still held) → action re-queues without
		// issuing a new Delete.
		err = action(ctx, rr)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("waiting for CertManager/cluster"))

		// Phase 3: Simulate cert-manager-operator removing its finalizers.
		got.SetFinalizers(nil)
		g.Expect(cli.Update(ctx, got)).To(Succeed())

		// Wait for the API server to remove the CR once all finalizers are gone.
		g.Eventually(func() error {
			return cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got)
		}).Should(MatchError(ContainSubstring("not found")))

		// With the CR gone, the action must return nil.
		err = action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
	})
}

// certManagerCR builds a minimal CertManager/cluster Unstructured object.
func certManagerCR(ownerRefs []metav1.OwnerReference, finalizers []string) *unstructured.Unstructured {
	cm := &unstructured.Unstructured{}
	cm.SetGroupVersionKind(gvk.CertManagerV1Alpha1)
	cm.SetName("cluster")
	if len(ownerRefs) > 0 {
		cm.SetOwnerReferences(ownerRefs)
	}
	if len(finalizers) > 0 {
		cm.SetFinalizers(finalizers)
	}
	return cm
}

// ownerRef builds a minimal valid OwnerReference with the given UID.
// APIVersion, Kind, and Name are required by the Kubernetes API server, but
// the action only checks ref.UID, so these values are arbitrary placeholders.
func ownerRef(uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "test.opendatahub.io/v1",
		Kind:       "TestPlatformObject",
		Name:       "test-owner",
		UID:        uid,
	}
}
