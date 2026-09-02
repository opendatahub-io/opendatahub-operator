package kueueoperator_test

import (
	"embed"
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
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

	cli, err := newKueueOperatorClient(renderDSC(operatorv1.Managed))
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

	cli, err := newKueueOperatorClient(renderDSC(operatorv1.Unmanaged))
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
		renderDSC(operatorv1.Unmanaged),
		renderSubscription(t, "openshift-kueue-operator"),
	)
	g.Expect(err).ToNot(HaveOccurred())

	err = kueueoperatorgate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestCheck_PassesWhenKueueRemoved(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := newKueueOperatorClient(renderDSC(operatorv1.Removed))
	g.Expect(err).ToNot(HaveOccurred())

	err = kueueoperatorgate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestCheck_UsesDSCStateWhenInternalKueueCRIsStale(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := newKueueOperatorClient(
		renderDSC(operatorv1.Unmanaged),
		renderKueue(t, "Managed"),
		renderSubscription(t, "openshift-kueue-operator"),
	)
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

func renderDSC(managementState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
	}
	dsc.Spec.Components.Kueue.ManagementState = managementState

	return dsc
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
