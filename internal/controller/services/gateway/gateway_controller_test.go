//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// TestGatewayCRDWatchPredicate pins the set of CRDs whose installation or removal re-triggers a
// GatewayConfig reconcile. The cert-manager Certificate CRD matters because XKS certificate
// handling is blocked until cert-manager is installed; without this event a cluster that installs
// cert-manager after the operator started would wait for an unrelated reconcile.
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

func TestGatewayCertManagerPrecondition(t *testing.T) {
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	for _, tc := range []struct {
		name        string
		certificate *infrav1.CertificateSpec
		oidc        *serviceApi.OIDCConfig
		wantStop    bool
	}{
		{name: "provided TLS without auth proxy", certificate: &infrav1.CertificateSpec{Type: infrav1.Provided}},
		{name: "provided TLS with auth proxy", certificate: &infrav1.CertificateSpec{Type: infrav1.Provided}, oidc: &serviceApi.OIDCConfig{}, wantStop: true},
		{name: "self-signed TLS", certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned}, wantStop: true},
		{name: "default TLS", wantStop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					Domain:      "apps.example.com",
					IngressMode: serviceApi.IngressModeLoadBalancer,
					Certificate: tc.certificate,
					OIDC:        tc.oidc,
				},
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   gatewayConfig,
				Conditions: conditions.NewManager(gatewayConfig, ReadyConditionType),
			}

			stop := precondition.RunAll(t.Context(), rr, []precondition.PreCondition{gatewayCertManagerPrecondition()})
			g.Expect(stop).To(Equal(tc.wantStop))
			dependency := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(dependency).NotTo(BeNil())
			if tc.wantStop {
				g.Expect(dependency.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(dependency.Message).To(Equal("cert-manager Certificate CRD is required for XKS certificate issuance"))
				return
			}

			g.Expect(dependency.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(createGatewayInfrastructure(t.Context(), rr)).To(Succeed())
			g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
			g.Expect(rr.Resources).To(HaveLen(2))
			g.Expect(rr.Resources[0].GroupVersionKind()).To(Equal(gvk.GatewayClass))
			g.Expect(rr.Resources[1].GroupVersionKind()).To(Equal(gvk.KubernetesGateway))
			g.Expect(rr.Resources[1].GetName()).To(Equal(GetDefaultGatewayName()))
			g.Expect(rr.Resources[1].GetNamespace()).To(Equal(GetGatewayNamespace()))
			g.Expect(rr.Templates).To(BeEmpty(), "AuthModeNone should not queue auth proxy resources")
		})
	}
}

func TestGatewayCertManagerPreconditionSkippedOnOpenShift(t *testing.T) {
	g := NewWithT(t)
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned},
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditions.NewManager(gatewayConfig, ReadyConditionType),
	}

	g.Expect(precondition.RunAll(t.Context(), rr, []precondition.PreCondition{gatewayCertManagerPrecondition()})).To(BeFalse())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable).Status).To(Equal(metav1.ConditionTrue))
}

func TestGatewayCertManagerPreconditionClearsDependencyAfterSwitchToProvided(t *testing.T) {
	g := NewWithT(t)
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned},
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditions.NewManager(gatewayConfig, ReadyConditionType),
	}
	pc := gatewayCertManagerPrecondition()

	g.Expect(precondition.RunAll(t.Context(), rr, []precondition.PreCondition{pc})).To(BeTrue())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable).Status).To(Equal(metav1.ConditionFalse))

	gatewayConfig.Spec.Certificate.Type = infrav1.Provided
	rr.Conditions.Reset()
	g.Expect(precondition.RunAll(t.Context(), rr, []precondition.PreCondition{pc})).To(BeFalse())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable).Status).To(Equal(metav1.ConditionTrue))
}
