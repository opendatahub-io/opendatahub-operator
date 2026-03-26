package servicemesh //nolint:testpackage

import (
	"context"
	"errors"
	"testing"

	"github.com/onsi/gomega"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func newTestReconciliationRequest(
	cl client.Client,
	controlPlaneNamespace string,
	authNamespace string,
	appNamespace string, //nolint:unparam
) *odhtypes.ReconciliationRequest {
	sm := &serviceApi.ServiceMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.ServiceMeshInstanceName,
		},
		Spec: serviceApi.ServiceMeshSpec{
			ControlPlane: serviceApi.ServiceMeshControlPlaneSpec{
				Name:      "data-science-smcp",
				Namespace: controlPlaneNamespace,
			},
			Auth: serviceApi.ServiceMeshAuthSpec{
				Namespace: authNamespace,
			},
		},
	}

	dsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: appNamespace,
		},
	}

	return &odhtypes.ReconciliationRequest{
		Client:   cl,
		Instance: sm,
		DSCI:     dsci,
	}
}

func newSMMR(namespace string, members []string) *unstructured.Unstructured {
	smmr := &unstructured.Unstructured{}
	smmr.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	smmr.SetName("default")
	smmr.SetNamespace(namespace)

	if members != nil {
		membersInterface := make([]interface{}, len(members))
		for i, m := range members {
			membersInterface[i] = m
		}
		_ = unstructured.SetNestedSlice(smmr.Object, membersInterface, "spec", "members")
	}

	return smmr
}

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = serviceApi.AddToScheme(s)
	_ = dsciv1.AddToScheme(s)
	return s
}

func TestCleanupSMMR_SoleMember(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", []string{"redhat-ods-applications-auth-provider"})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was deleted
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(k8serr.IsNotFound(err)).Should(gomega.BeTrue(), "SMMR should be deleted")
}

func TestCleanupSMMR_EmptyMembers(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", []string{})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was deleted
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(k8serr.IsNotFound(err)).Should(gomega.BeTrue(), "SMMR should be deleted")
}

func TestCleanupSMMR_NilMembers(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", nil)
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was deleted
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(k8serr.IsNotFound(err)).Should(gomega.BeTrue(), "SMMR should be deleted")
}

func TestCleanupSMMR_NotFound(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	// No SMMR object created
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
}

func TestCleanupSMMR_NoMatchError(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	// Use interceptor to simulate CRD not installed (NoResourceMatchError)
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				u, ok := obj.(*unstructured.Unstructured)
				if ok && u.GetKind() == "ServiceMeshMemberRoll" {
					return &meta.NoResourceMatchError{
						PartialResource: schema.GroupVersionResource{
							Group:    "maistra.io",
							Version:  "v1",
							Resource: "servicemeshmemberrolls",
						},
					}
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
}

func TestCleanupSMMR_MultipleMembers(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", []string{"redhat-ods-applications-auth-provider", "my-other-app"})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was NOT deleted
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(err).ShouldNot(gomega.HaveOccurred(), "SMMR should still exist")
}

func TestCleanupSMMR_OnlyNonRhoaiMembers(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", []string{"my-other-app"})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was NOT deleted
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(err).ShouldNot(gomega.HaveOccurred(), "SMMR should still exist")
}

func TestCleanupSMMR_DeletionFails(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", []string{"redhat-ods-applications-auth-provider"})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				u, ok := obj.(*unstructured.Unstructured)
				if ok && u.GetKind() == "ServiceMeshMemberRoll" {
					return errors.New("transient API error")
				}
				return client.Delete(ctx, obj, opts...)
			},
		}).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).Should(gomega.HaveOccurred())
	g.Expect(err).Should(gomega.MatchError(gomega.ContainSubstring("failed to delete ServiceMeshMemberRoll")))
}

func TestCleanupSMMR_CustomNamespaces(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	customControlPlane := "custom-mesh-system"
	customAuth := "my-custom-auth"

	smmr := newSMMR(customControlPlane, []string{customAuth})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	rr := newTestReconciliationRequest(cl, customControlPlane, customAuth, "redhat-ods-applications")

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was deleted from the custom namespace
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: customControlPlane}, result)
	g.Expect(k8serr.IsNotFound(err)).Should(gomega.BeTrue(), "SMMR should be deleted from custom namespace")
}

func TestCleanupSMMR_DefaultAuthNamespace(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	// When auth namespace is empty, it defaults to <appNamespace>-auth-provider
	appNamespace := "redhat-ods-applications"
	defaultAuthNs := appNamespace + "-auth-provider"

	smmr := newSMMR("istio-system", []string{defaultAuthNs})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	// Set auth namespace to empty to test default behavior
	rr := newTestReconciliationRequest(cl, "istio-system", "", appNamespace)

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was deleted
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(k8serr.IsNotFound(err)).Should(gomega.BeTrue(), "SMMR should be deleted when using default auth namespace")
}

func TestCleanupSMMR_Idempotent(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", []string{"redhat-ods-applications-auth-provider"})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	rr := newTestReconciliationRequest(cl, "istio-system", "redhat-ods-applications-auth-provider", "redhat-ods-applications")

	// First call — deletes the SMMR
	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Second call — SMMR already gone, should not error
	err = cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Third call — still idempotent
	err = cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
}

// TestCleanupSMMR_NilDSCI_FetchedFromCluster verifies that when rr.DSCI is nil (as set
// by the reconciler delete path) but the DSCI still exists on the cluster, the function
// fetches the DSCI to compute the default auth namespace and deletes the SMMR correctly.
func TestCleanupSMMR_NilDSCI_FetchedFromCluster(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	appNamespace := "redhat-ods-applications"
	defaultAuthNs := appNamespace + "-auth-provider"

	dsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: appNamespace,
		},
	}
	smmr := newSMMR("istio-system", []string{defaultAuthNs})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(dsci, smmr).
		Build()

	// Simulate the reconciler delete path: rr.DSCI is nil, auth namespace not set in spec
	rr := newTestReconciliationRequest(cl, "istio-system", "", appNamespace)
	rr.DSCI = nil

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was deleted
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(k8serr.IsNotFound(err)).Should(gomega.BeTrue(), "SMMR should be deleted when DSCI is fetched from cluster")
}

// TestCleanupSMMR_NilDSCI_DSCINotFound verifies that when rr.DSCI is nil and the DSCI
// does not exist on the cluster, the function skips cleanup conservatively (returns nil
// without deleting the SMMR) to avoid removing user-managed members.
func TestCleanupSMMR_NilDSCI_DSCINotFound(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := t.Context()

	smmr := newSMMR("istio-system", []string{"redhat-ods-applications-auth-provider"})
	cl := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(smmr).
		Build()

	// Simulate the reconciler delete path: rr.DSCI is nil, no DSCI on cluster, auth namespace not set in spec
	rr := newTestReconciliationRequest(cl, "istio-system", "", "redhat-ods-applications")
	rr.DSCI = nil

	err := cleanupServiceMeshMemberRoll(ctx, rr)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// Verify SMMR was NOT deleted (conservative skip when auth namespace is unknown)
	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(gvk.ServiceMeshMemberRoll)
	err = cl.Get(ctx, client.ObjectKey{Name: "default", Namespace: "istio-system"}, result)
	g.Expect(err).ShouldNot(gomega.HaveOccurred(), "SMMR should be preserved when DSCI is not available")
}
