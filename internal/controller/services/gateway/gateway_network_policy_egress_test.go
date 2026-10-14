//go:build !integration

//nolint:testpackage
package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"text/template"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
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
		Conditions: conditionstest.NewManager(&gatewayConfigConditionsAccessor{}, ReadyConditionType),
	}
	var requeueErr odherrors.RequeueAfterError
	g.Expect(errors.As(createNetworkPolicy(t.Context(), rr), &requeueErr)).To(BeTrue(),
		"egress resolution failure must schedule a timed requeue")
	g.Expect(requeueErr.After).To(Equal(authProxyEgressRequeueDelay))
	g.Expect(rr.Templates).To(HaveLen(1))
	rules, ok := rr.Extensions[authProxyEgressRulesKey].([]networkingv1.NetworkPolicyEgressRule)
	g.Expect(ok).To(BeTrue())
	g.Expect(rules).To(BeEmpty())
	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal("AuthProxyEgressUnavailable"))
}

func TestAuthProxyEgressConditionRecoversAfterDestinationsResolve(t *testing.T) {
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
		Conditions: conditionstest.NewManager(&gatewayConfigConditionsAccessor{}, ReadyConditionType),
	}

	// Reconciliation while the cluster Network is missing fails closed.
	var requeueErr odherrors.RequeueAfterError
	g.Expect(errors.As(createNetworkPolicy(t.Context(), rr), &requeueErr)).To(BeTrue())
	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(authProxyEgressUnavailableReason))

	// Next reconciliation after the dependencies recovered must release the
	// recorded failure so the status action can mark the condition ready.
	apiPort := int32(6443)
	apiPortName := "https"
	tcpProtocol := corev1.ProtocolTCP
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
		Ports: []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(6443)}},
	}}
	apiEndpoints := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "kubernetes-1", Namespace: "default", Labels: map[string]string{discoveryv1.LabelServiceName: "kubernetes"},
	}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: &apiPortName, Port: &apiPort, Protocol: &tcpProtocol}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"192.0.2.1"}}},
	}
	recoveredCli, err := fakeclient.New(fakeclient.WithObjects(config, authentication, network, dnsService, apiService, apiEndpoints))
	g.Expect(err).NotTo(HaveOccurred())
	rr.Client = recoveredCli

	g.Expect(createNetworkPolicy(t.Context(), rr)).To(Succeed())
	ready = rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(ready.Reason).To(Equal("Ready"))
	rules, ok := rr.Extensions[authProxyEgressRulesKey].([]networkingv1.NetworkPolicyEgressRule)
	g.Expect(ok).To(BeTrue())
	g.Expect(rules).NotTo(BeEmpty())
}

// TestUnknownAuthModeFailsClosed pins the fail-closed contract for an
// unreadable Authentication/cluster on OpenShift: the actions must not abort
// with a plain error that leaves a previously applied allow-all egress policy
// in place. The proxy action records a bounded requeue and marks the mode
// undetermined so GC keeps the proxy Deployment, while the policy action
// queues deny-all egress and reports the failure on the Ready condition.
func TestUnknownAuthModeFailsClosed(t *testing.T) {
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
	// No Authentication object: the cluster authentication mode is unknown.
	cli, err := fakeclient.New(fakeclient.WithObjects(config))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{
		Client: cli, Instance: config,
		Conditions: conditionstest.NewManager(&gatewayConfigConditionsAccessor{}, ReadyConditionType),
	}

	// The proxy action must not abort the chain: it records a bounded requeue
	// and marks the mode undetermined for the GC predicate.
	var proxyRequeue odherrors.RequeueAfterError
	g.Expect(errors.As(createKubeAuthProxyInfrastructure(t.Context(), rr), &proxyRequeue)).To(BeTrue(),
		"an unknown auth mode must schedule a timed requeue instead of aborting the action chain")
	g.Expect(proxyRequeue.After).To(Equal(authProxyEgressRequeueDelay))
	g.Expect(rr.Extensions[authProxyModeUndeterminedKey]).To(BeTrue(),
		"the proxy action must mark the mode undetermined for the GC predicate")

	// The policy action fails closed: deny-all egress is queued and the
	// service-specific Ready condition reports the failure.
	var policyRequeue odherrors.RequeueAfterError
	g.Expect(errors.As(createNetworkPolicy(t.Context(), rr), &policyRequeue)).To(BeTrue(),
		"an unknown auth mode must schedule a timed requeue instead of returning a plain error")
	g.Expect(policyRequeue.After).To(Equal(authProxyEgressRequeueDelay))
	g.Expect(rr.Templates).To(HaveLen(1))
	rules, ok := rr.Extensions[authProxyEgressRulesKey].([]networkingv1.NetworkPolicyEgressRule)
	g.Expect(ok).To(BeTrue())
	g.Expect(rules).To(BeEmpty())
	ready := rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(authProxyEgressUnavailableReason))
	g.Expect(ready.Message).To(ContainSubstring("Cannot detect cluster authentication mode"))

	// GC must not collect the kube-auth-proxy Deployment while the mode is
	// undetermined, even though it is not among the desired resources.
	proxyDeployment := unstructured.Unstructured{}
	proxyDeployment.SetGroupVersionKind(gvk.Deployment)
	proxyDeployment.SetName(KubeAuthProxyName)
	proxyDeployment.SetNamespace(GetGatewayNamespace())
	collect, err := gatewayGCObjectPredicate(rr, proxyDeployment)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(collect).To(BeFalse(), "the proxy Deployment must be kept while the auth mode is undetermined")

	// Without the marker the omitted proxy Deployment is collected as before.
	delete(rr.Extensions, authProxyModeUndeterminedKey)
	collect, err = gatewayGCObjectPredicate(rr, proxyDeployment)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(collect).To(BeTrue(), "an omitted proxy Deployment is collected once the mode is known")

	// Recovery: when Authentication becomes readable and destinations resolve,
	// the recorded failure is released and resolved rules are restored.
	authentication := &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
		Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
	}
	apiPort := int32(6443)
	apiPortName := "https"
	tcpProtocol := corev1.ProtocolTCP
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
		Ports: []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(6443)}},
	}}
	apiEndpoints := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "kubernetes-1", Namespace: "default", Labels: map[string]string{discoveryv1.LabelServiceName: "kubernetes"},
	}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: &apiPortName, Port: &apiPort, Protocol: &tcpProtocol}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"192.0.2.1"}}},
	}
	recoveredCli, err := fakeclient.New(fakeclient.WithObjects(config, authentication, network, dnsService, apiService, apiEndpoints))
	g.Expect(err).NotTo(HaveOccurred())
	rr.Client = recoveredCli

	g.Expect(createNetworkPolicy(t.Context(), rr)).To(Succeed())
	ready = rr.Conditions.GetCondition(ReadyConditionType)
	g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(ready.Reason).To(Equal("Ready"))
	rules, ok = rr.Extensions[authProxyEgressRulesKey].([]networkingv1.NetworkPolicyEgressRule)
	g.Expect(ok).To(BeTrue())
	g.Expect(rules).NotTo(BeEmpty())
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

// TestAuthProxyAPIRulesRejectsUnvalidatedEndpointSliceDestinations pins the
// EndpointSlice cross-reference contract: only ports matching the kubernetes
// Service's 443 targetPort and only ready (or readiness-unknown) endpoints may
// widen the egress allowlist.
func TestAuthProxyAPIRulesRejectsUnvalidatedEndpointSliceDestinations(t *testing.T) {
	g := NewWithT(t)

	apiPort := int32(6443)
	apiPortName := "https"
	tcpProtocol := corev1.ProtocolTCP
	roguePort := int32(8443)
	roguePortName := "metrics"
	notReady := false
	ready := true
	apiService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"}, Spec: corev1.ServiceSpec{
		ClusterIP: "172.30.0.1", ClusterIPs: []string{"172.30.0.1"},
		Ports: []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(6443)}},
	}}
	// A slice labeled for the kubernetes Service whose second port does not
	// match the Service's 443 targetPort, with one endpoint reporting not
	// ready alongside ready and readiness-unknown endpoints.
	apiEndpoints := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "kubernetes-1", Namespace: "default", Labels: map[string]string{discoveryv1.LabelServiceName: "kubernetes"},
	}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{Name: &apiPortName, Port: &apiPort, Protocol: &tcpProtocol},
			{Name: &roguePortName, Port: &roguePort, Protocol: &tcpProtocol},
		},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"192.0.2.1"}},
			{Addresses: []string{"192.0.2.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
			{Addresses: []string{"192.0.2.3"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(apiService, apiEndpoints))
	g.Expect(err).NotTo(HaveOccurred())

	rules, err := authProxyAPIRules(t.Context(), cli)
	g.Expect(err).NotTo(HaveOccurred())
	destinations := make([]string, 0, len(rules))
	for _, rule := range rules {
		g.Expect(rule.To).To(HaveLen(1))
		g.Expect(rule.Ports).To(HaveLen(1))
		destinations = append(destinations,
			fmt.Sprintf("%s:%d", rule.To[0].IPBlock.CIDR, rule.Ports[0].Port.IntVal))
	}
	// Only the Service ClusterIP on 443 and the validated ready endpoints on
	// the targetPort are allowed; the unmatched port and the not-ready
	// endpoint must not widen the allowlist.
	g.Expect(rules).To(HaveLen(3))
	g.Expect(destinations).To(ConsistOf("172.30.0.1/32:443", "192.0.2.1/32:6443", "192.0.2.3/32:6443"))
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
	apiPortName := "https"
	tcpProtocol := corev1.ProtocolTCP
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
		Ports: []corev1.ServicePort{{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(6443)}},
	}}
	apiEndpoints := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "kubernetes-1", Namespace: "default", Labels: map[string]string{discoveryv1.LabelServiceName: "kubernetes"},
	}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: &apiPortName, Port: &apiPort, Protocol: &tcpProtocol}},
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
