//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestGatewaySelfSignedCertificateProvider(t *testing.T) {
	for _, tc := range []struct {
		name            string
		xks             bool
		certManager     bool
		wantCertificate bool
	}{
		{name: "XKS with cert-manager", xks: true, certManager: true, wantCertificate: true},
		{name: "XKS without cert-manager", xks: true},
		{name: "OpenShift with cert-manager", certManager: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			originalClusterInfo := cluster.GetClusterInfo()
			t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
			info := cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift}
			if tc.xks {
				info.Type = cluster.ClusterTypeKubernetes
			}
			cluster.SetClusterInfo(info)

			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned},
				},
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig), fakeclient.WithGVKs(
				fakeclient.GVKMapping{GVK: gvk.CertManagerCertificate, Scope: meta.RESTScopeNamespace},
			))
			g.Expect(err).NotTo(HaveOccurred())
			if tc.certManager {
				g.Expect(cli.Create(t.Context(), &extv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: gvk.CertManagerCertificateCRDName},
				})).To(Succeed())
			}
			rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}
			secretName, err := handleCertificates(t.Context(), rr, gatewayConfig, "gateway.example.com")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(secretName).To(Equal(serviceApi.GatewayConfigName + "-tls"))

			secret := &corev1.Secret{}
			err = cli.Get(t.Context(), types.NamespacedName{Name: secretName, Namespace: GetGatewayNamespace()}, secret)
			if tc.wantCertificate {
				g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
				g.Expect(rr.Resources).To(HaveLen(1))
				g.Expect(rr.Resources[0].GroupVersionKind()).To(Equal(gvk.CertManagerCertificate))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(rr.Resources).To(BeEmpty())
				g.Expect(secret.Type).To(Equal(corev1.SecretTypeTLS))
				g.Expect(secret.Data[corev1.TLSCertKey]).NotTo(BeEmpty())
				g.Expect(secret.Data[corev1.TLSPrivateKeyKey]).NotTo(BeEmpty())
			}
		})
	}
}

func TestXKSReconcileWithoutDomainStopsCleanly(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	ctx := t.Context()
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			IngressMode: serviceApi.IngressModeLoadBalancer,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).NotTo(HaveOccurred())

	accessor := &gatewayConfigConditionsAccessor{}
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditions.NewManager(accessor, ReadyConditionType),
	}

	g.Expect(createGatewayInfrastructure(ctx, rr)).To(Succeed())
	g.Expect(createKubeAuthProxyInfrastructure(ctx, rr)).To(Succeed())
	g.Expect(createEnvoyFilter(ctx, rr)).To(Succeed())
	g.Expect(createNetworkPolicy(ctx, rr)).To(Succeed())
	g.Expect(syncGatewayConfigStatus(ctx, rr)).To(Succeed())

	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(status.NotReadyReason))
	g.Expect(ready.Message).To(Equal(status.GatewayDomainRequiredMessage))

	g.Expect(rr.Resources).To(BeEmpty(), "no gateway resources should be queued without domain")
	g.Expect(rr.Templates).To(BeEmpty(), "no templates should be queued without domain")

	gateway := &gwapiv1.Gateway{}
	err = cli.Get(ctx, types.NamespacedName{
		Name:      GetDefaultGatewayName(),
		Namespace: GetGatewayNamespace(),
	}, gateway)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "Gateway CR should not be created without domain")

	gatewayClass := &gwapiv1.GatewayClass{}
	err = cli.Get(ctx, types.NamespacedName{Name: GatewayClassName}, gatewayClass)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "GatewayClass should not be created without domain")
}

func TestXKSReconcileRejectsOpenShiftOnlyValues(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	ctx := t.Context()
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Domain:      "apps.example.com",
			IngressMode: serviceApi.IngressModeOcpRoute,
			Certificate: &infrav1.CertificateSpec{Type: infrav1.OpenshiftDefaultIngress},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).NotTo(HaveOccurred())

	accessor := &gatewayConfigConditionsAccessor{}
	rr := &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   gatewayConfig,
		Conditions: conditions.NewManager(accessor, ReadyConditionType),
	}

	g.Expect(createGatewayInfrastructure(ctx, rr)).To(Succeed())
	g.Expect(createKubeAuthProxyInfrastructure(ctx, rr)).To(Succeed())
	g.Expect(createEnvoyFilter(ctx, rr)).To(Succeed())
	g.Expect(createNetworkPolicy(ctx, rr)).To(Succeed())
	g.Expect(syncGatewayConfigStatus(ctx, rr)).To(Succeed())

	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(status.NotReadyReason))
	g.Expect(ready.Message).To(ContainSubstring(status.GatewayUnsupportedCertTypeOnKubernetesMessage))
	g.Expect(ready.Message).To(ContainSubstring(status.GatewayUnsupportedIngressModeOnKubernetesMessage))
	g.Expect(rr.Resources).To(BeEmpty(), "no gateway resources should be queued for unsupported XKS spec")
}
