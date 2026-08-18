package kueueoperator_test

import (
	"embed"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	kueueoperatorgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kueueoperator"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

//go:embed resources
var resourcesFS embed.FS

func TestCheck_PassesWhenKueueNotConfigured(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := newKueueOperatorClient()
	g.Expect(err).ToNot(HaveOccurred())

	err = kueueoperatorgate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestCheck_BlocksWhenKueueManaged(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := newKueueOperatorClient(renderKueue(t, "Managed"))
	g.Expect(err).ToNot(HaveOccurred())

	err = kueueoperatorgate.Check(t.Context(), cli, "", "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *kueueoperatorgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.ManagedStateUnsupported).To(BeTrue())
}

func TestCheck_BlocksWhenKueueUnmanagedWithoutOperator(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := newKueueOperatorClient(renderKueue(t, "Unmanaged"))
	g.Expect(err).ToNot(HaveOccurred())

	err = kueueoperatorgate.Check(t.Context(), cli, "", "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *kueueoperatorgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.MissingKueueOperatorSubscription).To(BeTrue())
}

func TestCheck_PassesWhenKueueUnmanagedWithOperator(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := newKueueOperatorClient(
		renderKueue(t, "Unmanaged"),
		renderSubscription(t, "openshift-kueue-operator"),
	)
	g.Expect(err).ToNot(HaveOccurred())

	err = kueueoperatorgate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestCheck_PassesWhenKueueRemoved(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := newKueueOperatorClient(renderKueue(t, "Removed"))
	g.Expect(err).ToNot(HaveOccurred())

	err = kueueoperatorgate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}

func newKueueOperatorClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.Kueue, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: gvk.Subscription, Scope: meta.RESTScopeNamespace},
		),
	)
}

func renderKueue(t *testing.T, managementState string) client.Object {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/kueue.tmpl.yaml", map[string]any{
		"ManagementState": managementState,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}

func renderSubscription(t *testing.T, namespace string) client.Object {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/subscription.tmpl.yaml", map[string]any{
		"Namespace": namespace,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}
