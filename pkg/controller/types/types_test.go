package types_test

import (
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestReconciliationRequest_AddResource(t *testing.T) {
	g := NewWithT(t)

	cl, err := fakeclient.New()
	g.Expect(err).ToNot(HaveOccurred())

	rr := types.ReconciliationRequest{Client: cl}

	g.Expect(rr.AddResources(&unstructured.Unstructured{})).To(HaveOccurred())
	g.Expect(rr.Resources).To(BeEmpty())

	g.Expect(rr.AddResources(&corev1.ConfigMap{})).ToNot(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(1))

	g.Expect(rr.AddResources([]client.Object{}...)).ToNot(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(1))
}

func TestReconciliationRequest_ForEachResource_UpdateSome(t *testing.T) {
	g := NewWithT(t)

	cl, err := fakeclient.New()
	g.Expect(err).ToNot(HaveOccurred())

	rr := types.ReconciliationRequest{Client: cl}
	g.Expect(rr.AddResources(&corev1.ConfigMap{})).ToNot(HaveOccurred())
	g.Expect(rr.AddResources(&corev1.Secret{})).ToNot(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(2))

	val := xid.New().String()

	g.Expect(
		rr.ForEachResource(func(u *unstructured.Unstructured) (bool, error) {
			if u.GroupVersionKind() == gvk.ConfigMap {
				return false, nil
			}

			if err := unstructured.SetNestedField(u.Object, val, "data", "key"); err != nil {
				return false, err
			}

			return true, nil
		}),
	).ToNot(HaveOccurred())

	g.Expect(rr.Resources).To(HaveLen(2))
	g.Expect(rr.Resources[0].Object).To(jq.Match(`has("data") | not`))
	g.Expect(rr.Resources[1].Object).To(jq.Match(`.data.key == "%s"`, val))
}

func TestReconciliationRequest_ForEachResource_UpdateAll(t *testing.T) {
	g := NewWithT(t)

	cl, err := fakeclient.New()
	g.Expect(err).ToNot(HaveOccurred())

	rr := types.ReconciliationRequest{Client: cl}
	g.Expect(rr.AddResources(&corev1.ConfigMap{})).ToNot(HaveOccurred())
	g.Expect(rr.AddResources(&corev1.Secret{})).ToNot(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(2))

	val := xid.New().String()

	g.Expect(
		rr.ForEachResource(func(u *unstructured.Unstructured) (bool, error) {
			if err := unstructured.SetNestedField(u.Object, val, "data", "key"); err != nil {
				return false, err
			}

			return false, nil
		}),
	).ToNot(HaveOccurred())

	g.Expect(rr.Resources).To(And(
		HaveLen(2),
		HaveEach(jq.Match(`.data.key == "%s"`, val)),
	))
}

func TestReconciliationRequest_RemoveResources(t *testing.T) {
	g := NewWithT(t)

	cl, err := fakeclient.New()
	g.Expect(err).ToNot(HaveOccurred())

	// Create a ReconciliationRequest with some resources
	rr := types.ReconciliationRequest{Client: cl}

	err = rr.AddResources(&corev1.ConfigMap{}, &corev1.Secret{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(2))

	// Remove all ConfigMaps using the predicate function
	err = rr.RemoveResources(func(u *unstructured.Unstructured) bool {
		return u.GroupVersionKind() == gvk.ConfigMap
	})

	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(rr.Resources).To(And(
		HaveLen(1),
		HaveEach(jq.Match(`.kind == "%s"`, gvk.Secret.Kind)),
	))
}
