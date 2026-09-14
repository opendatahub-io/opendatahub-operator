//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

// TestGatewayCRDWatchPredicate pins the set of CRDs whose installation or removal re-triggers a
// GatewayConfig reconcile. The cert-manager Certificate CRD matters because certificate handling
// chooses between cert-manager and the self-signed fallback on every reconcile: without this
// event a cluster that installs cert-manager after the operator started keeps serving the
// self-signed fallback until something unrelated triggers a reconcile.
func TestGatewayCRDWatchPredicate(t *testing.T) {
	t.Parallel()

	pred := gatewayCRDWatchPredicate()

	for _, tc := range []struct {
		name      string
		crdName   string
		wantMatch bool
	}{
		{name: "cert-manager Certificate CRD", crdName: gvk.CertManagerCertificateCRDName, wantMatch: true},
		{name: "Dashboard CRD", crdName: gvk.DashboardComponentCRDName, wantMatch: true},
		{name: "unrelated CRD", crdName: "widgets.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			crd := &extv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: tc.crdName}}

			g.Expect(pred.Create(event.CreateEvent{Object: crd})).To(Equal(tc.wantMatch))
			g.Expect(pred.Update(event.UpdateEvent{ObjectOld: crd, ObjectNew: crd})).To(Equal(tc.wantMatch))
			g.Expect(pred.Delete(event.DeleteEvent{Object: crd})).To(Equal(tc.wantMatch))
		})
	}
}
