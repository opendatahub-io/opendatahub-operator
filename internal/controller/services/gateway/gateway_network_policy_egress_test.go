//go:build !integration

//nolint:testpackage
package gateway

import (
	"bytes"
	"testing"
	"text/template"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	templateutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestOpenShiftMissingNetworkQueuesDenyAllEgress(t *testing.T) {
	original := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	g := NewWithT(t)
	config := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Domain: "example.com", IngressMode: serviceApi.IngressModeLoadBalancer,
		},
	}
	authentication := &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
		Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(config, authentication))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client: cli, Instance: config,
		Conditions: conditions.NewManager(&gatewayConfigConditionsAccessor{}, ReadyConditionType),
	}
	g.Expect(createNetworkPolicy(t.Context(), rr)).To(Succeed())
	g.Expect(rr.Templates).To(HaveLen(1))
	rules, ok := rr.Extensions[authProxyEgressRulesKey].([]networkingv1.NetworkPolicyEgressRule)
	g.Expect(ok).To(BeTrue())
	g.Expect(rules).To(BeEmpty())
	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal("AuthProxyEgressUnavailable"))
}

func TestAuthProxyNetworkPolicyDenyAllEgressRendering(t *testing.T) {
	g := NewWithT(t)
	contents, err := gatewayResources.ReadFile(networkPolicyTemplate)
	g.Expect(err).NotTo(HaveOccurred())
	tmpl, err := template.New("auth-proxy-policy").Funcs(templateutils.TextTemplateFuncMap()).
		Option("missingkey=error").Parse(string(contents))
	g.Expect(err).NotTo(HaveOccurred())
	data := map[string]any{
		"KubeAuthProxyServiceName": KubeAuthProxyName,
		"GatewayNamespace":         "test-gateway",
		"ComponentLabelKey":        "app.kubernetes.io/component",
		"ComponentLabelValue":      "authentication",
		"GatewayNameLabelKey":      "gateway.networking.k8s.io/gateway-name",
		"GatewayName":              "test-gateway",
		"GatewayFilters":           gatewayEnvoyFilterTargets(nil),
		"GatewayHTTPSPort":         8443,
		"AuthProxyEgressRules":     make([]networkingv1.NetworkPolicyEgressRule, 0),
	}
	var rendered bytes.Buffer
	g.Expect(tmpl.Execute(&rendered, data)).To(Succeed())
	policy := &networkingv1.NetworkPolicy{}
	g.Expect(yaml.UnmarshalStrict(rendered.Bytes(), policy)).To(Succeed())
	g.Expect(policy.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))
	g.Expect(policy.Spec.Egress).To(BeEmpty())
}

func TestResolveAuthProxyEgressForKubernetesAllowsAll(t *testing.T) {
	original := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})

	cli, err := fakeclient.New()
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())

	rules, err := resolveAuthProxyEgress(t.Context(), cli, cluster.AuthModeOIDC)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rules).To(Equal([]networkingv1.NetworkPolicyEgressRule{{}}))
}

func TestResolveAuthProxyEgressForOpenShift(t *testing.T) {
	original := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	g := NewWithT(t)
	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	oidcRules, err := resolveAuthProxyEgress(
		t.Context(), cli, cluster.AuthModeOIDC,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(oidcRules).To(Equal([]networkingv1.NetworkPolicyEgressRule{{}}))

	apiPort := int32(6443)
	network := &configv1.Network{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	network.Status.ClusterNetwork = []configv1.ClusterNetworkEntry{{CIDR: "10.244.0.0/16"}}
	network.Status.ServiceNetwork = []string{"10.96.0.0/12"}
	dnsService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "dns-default", Namespace: "openshift-dns"}, Spec: corev1.ServiceSpec{
		ClusterIP: "172.30.0.10", ClusterIPs: []string{"172.30.0.10"}, Selector: map[string]string{"dns.operator.openshift.io/daemonset-dns": "default"},
		Ports: []corev1.ServicePort{
			{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt32(53)},
			{Name: "dns-tcp", Port: 53, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(53)},
		},
	}}
	apiService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"}, Spec: corev1.ServiceSpec{
		ClusterIP: "172.30.0.1", ClusterIPs: []string{"172.30.0.1"},
		Ports: []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP}},
	}}
	apiEndpoints := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "kubernetes-1", Namespace: "default", Labels: map[string]string{discoveryv1.LabelServiceName: "kubernetes"},
	}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Port: &apiPort}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"192.0.2.1"}}},
	}
	cli, err = fakeclient.New(fakeclient.WithObjects(network, dnsService, apiService, apiEndpoints))
	g.Expect(err).NotTo(HaveOccurred())
	oauthRules, err := resolveAuthProxyEgress(t.Context(), cli, cluster.AuthModeIntegratedOAuth)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(oauthRules).To(HaveLen(7)) // DNS pods/VIP, API VIP/endpoint, external HTTPS
	g.Expect(oauthRules[5].To[0].IPBlock.CIDR).To(Equal("192.0.2.1/32"))
	g.Expect(oauthRules[6].To[0].IPBlock).To(Equal(&networkingv1.IPBlock{
		CIDR: "0.0.0.0/0", Except: []string{"10.244.0.0/16", "10.96.0.0/12"},
	}))
	g.Expect(oauthRules[6].Ports[0].Port.IntVal).To(Equal(int32(443)))
}
