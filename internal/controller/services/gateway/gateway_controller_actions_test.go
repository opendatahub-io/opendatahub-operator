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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

// testIssuerRef is a deliberately non-default issuer so that assertions on a produced Certificate
// fail if name and kind are transposed, or if the per-GatewayConfig override is ignored in favour
// of the platform default.
var testIssuerRef = infrav1.IssuerRef{Name: "test-ca-issuer", Kind: "Issuer"}

// expectCertManagerCertificate asserts the full wiring of a produced cert-manager Certificate:
// its identity, the Secret cert-manager is told to populate, the SANs requested, and the issuer
// that signs it. Checking only the GVK would let a transposed argument at the call site through.
func expectCertManagerCertificate(
	t *testing.T,
	g Gomega,
	obj unstructured.Unstructured,
	name, namespace, secretName string,
	dnsNames []string,
	issuerRef infrav1.IssuerRef,
) {
	t.Helper()

	g.Expect(obj.GroupVersionKind()).To(Equal(gvk.CertManagerCertificate))
	g.Expect(obj.GetName()).To(Equal(name))
	g.Expect(obj.GetNamespace()).To(Equal(namespace))

	gotSecretName, found, err := unstructured.NestedString(obj.Object, "spec", "secretName")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue(), "Certificate must set spec.secretName")
	g.Expect(gotSecretName).To(Equal(secretName))

	gotDNSNames, found, err := unstructured.NestedStringSlice(obj.Object, "spec", "dnsNames")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue(), "Certificate must set spec.dnsNames")
	g.Expect(gotDNSNames).To(Equal(dnsNames))

	gotIssuerRef, found, err := unstructured.NestedStringMap(obj.Object, "spec", "issuerRef")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue(), "Certificate must set spec.issuerRef")
	g.Expect(gotIssuerRef).To(Equal(map[string]string{
		"name":  issuerRef.Name,
		"kind":  issuerRef.Kind,
		"group": gvk.CertManagerCertificate.Group,
	}))
}

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

			issuerRef := testIssuerRef
			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned, IssuerRef: &issuerRef},
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
				// The Certificate must name the same Secret the gateway listener consumes, and
				// carry the gateway FQDN as its SAN.
				expectCertManagerCertificate(t, g, rr.Resources[0],
					secretName, GetGatewayNamespace(), secretName,
					[]string{"gateway.example.com"}, issuerRef)
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

// TestKubeAuthProxyCertificateProvider covers the XKS kube-auth-proxy TLS branch in
// createKubeAuthProxyInfrastructure, in both directions: cert-manager present issues a
// Certificate, cert-manager absent falls back to an operator-generated self-signed Secret.
//
// That branch sits near the end of the action, behind an unsupported-spec rejection, domain
// resolution, auth-mode detection and credential setup. The existing XKS tests in this file all
// return before reaching it, so the GatewayConfig below is configured to pass every one of those
// gates, and each case asserts Ready=True with AuthProxyDeployedMessage - set only on the final
// line of the action - to prove the cert branch was actually exercised rather than skipped.
func TestKubeAuthProxyCertificateProvider(t *testing.T) {
	for _, tc := range []struct {
		name            string
		certManager     bool
		wantCertificate bool
	}{
		{name: "XKS with cert-manager", certManager: true, wantCertificate: true},
		{name: "XKS without cert-manager"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			originalClusterInfo := cluster.GetClusterInfo()
			t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
			cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

			issuerRef := testIssuerRef
			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					// Domain is required on XKS, otherwise the action returns at hostname resolution.
					Domain: "apps.example.com",
					// OIDC is what selects AuthModeOIDC on XKS; without it the action returns
					// early with AuthModeNone and never provisions a proxy certificate.
					OIDC: &serviceApi.OIDCConfig{
						IssuerURL: "https://oidc.example.com",
						ClientID:  "odh-gateway",
						ClientSecretRef: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "oidc-client-secret"},
							Key:                  "clientSecret",
						},
					},
					Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned, IssuerRef: &issuerRef},
				},
			}

			oidcSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "oidc-client-secret", Namespace: GetGatewayNamespace()},
				Data:       map[string][]byte{"clientSecret": []byte("oidc-secret-value")},
			}

			cli, err := fakeclient.New(
				fakeclient.WithObjects(gatewayConfig, oidcSecret),
				fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: gvk.CertManagerCertificate, Scope: meta.RESTScopeNamespace}),
			)
			g.Expect(err).NotTo(HaveOccurred())
			if tc.certManager {
				g.Expect(cli.Create(ctx, &extv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: gvk.CertManagerCertificateCRDName},
				})).To(Succeed())
			}

			rr := &odhtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   gatewayConfig,
				Conditions: conditions.NewManager(&gatewayConfigConditionsAccessor{}, ReadyConditionType),
			}

			g.Expect(createKubeAuthProxyInfrastructure(ctx, rr)).To(Succeed())

			ready := rr.Conditions.GetCondition(ReadyConditionType)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(ready.Message).To(Equal(status.AuthProxyDeployedMessage),
				"action must run to completion, otherwise the certificate branch was never reached")

			tlsSecret := &corev1.Secret{}
			err = cli.Get(ctx, types.NamespacedName{Name: KubeAuthProxyTLSName, Namespace: GetGatewayNamespace()}, tlsSecret)

			if tc.wantCertificate {
				g.Expect(k8serr.IsNotFound(err)).To(BeTrue(),
					"cert-manager owns the Secret; the operator must not pre-create it")
				g.Expect(rr.Resources).To(HaveLen(1))
				// The Certificate must name the Secret the kube-auth-proxy Deployment mounts, and
				// carry the in-cluster Service DNS name the EnvoyFilter dials for ext_authz.
				expectCertManagerCertificate(t, g, rr.Resources[0],
					KubeAuthProxyTLSName, GetGatewayNamespace(), KubeAuthProxyTLSName,
					[]string{KubeAuthProxyName + "." + GetGatewayNamespace() + ".svc.cluster.local"},
					issuerRef)
			} else {
				g.Expect(err).NotTo(HaveOccurred(), "self-signed fallback must create the TLS Secret")
				g.Expect(rr.Resources).To(BeEmpty())
				g.Expect(tlsSecret.Type).To(Equal(corev1.SecretTypeTLS))
				g.Expect(tlsSecret.Data[corev1.TLSCertKey]).NotTo(BeEmpty())
				g.Expect(tlsSecret.Data[corev1.TLSPrivateKeyKey]).NotTo(BeEmpty())
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
