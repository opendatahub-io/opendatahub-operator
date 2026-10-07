package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	authProxyEgressRulesKey = "authProxyEgressRules"
	dnsServicePort          = 53
	apiServiceName          = "kubernetes"
	apiServiceNamespace     = "default"

	openshiftDNSNamespace   = "openshift-dns"
	openshiftDNSServiceName = "dns-default"
)

// resolveAuthProxyEgress restricts egress for OpenShift OAuth. OIDC retains
// allow-all because standard NetworkPolicy cannot reliably represent its
// discovery destinations. Other cluster types retain allow-all because their
// cluster address ranges are not reliably available.
func resolveAuthProxyEgress(
	ctx context.Context,
	cli client.Client,
	mode cluster.AuthenticationMode,
) ([]networkingv1.NetworkPolicyEgressRule, error) {
	if cluster.GetClusterInfo().Type != cluster.ClusterTypeOpenShift || mode == cluster.AuthModeOIDC {
		return []networkingv1.NetworkPolicyEgressRule{{}}, nil
	}

	podCIDRs, serviceCIDRs, err := authProxyClusterCIDRs(ctx, cli)
	if err != nil {
		return nil, err
	}

	rules, err := authProxyDNSRules(ctx, cli)
	if err != nil {
		return nil, err
	}

	if mode == cluster.AuthModeIntegratedOAuth {
		apiRules, err := authProxyAPIRules(ctx, cli)
		if err != nil {
			return nil, err
		}
		rules = append(rules, apiRules...)
	}
	// NetworkPolicy has no FQDN peer. This is the baseline for the OpenShift
	// OAuth route, not an IdP-specific rule.
	for _, family := range []string{"ipv4", "ipv6"} {
		except := cidrsForFamily(append(append([]string{}, podCIDRs...), serviceCIDRs...), family)
		if len(except) == 0 {
			continue
		}
		cidr := "0.0.0.0/0"
		if family == "ipv6" {
			cidr = "::/0"
		}
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr, Except: except}}},
			Ports: []networkingv1.NetworkPolicyPort{networkPolicyPort(corev1.ProtocolTCP, intstr.FromInt32(443))},
		})
	}
	return rules, nil
}

func authProxyClusterCIDRs(ctx context.Context, cli client.Client) ([]string, []string, error) {
	var podCIDRs, serviceCIDRs []string
	network := &configv1.Network{}
	if err := cli.Get(ctx, types.NamespacedName{Name: "cluster"}, network); err != nil {
		return nil, nil, fmt.Errorf("get OpenShift cluster Network: %w", err)
	}
	for _, entry := range network.Status.ClusterNetwork {
		podCIDRs = append(podCIDRs, entry.CIDR)
	}
	serviceCIDRs = network.Status.ServiceNetwork
	if len(podCIDRs) == 0 || len(serviceCIDRs) == 0 {
		return nil, nil, errors.New("both Pod and Service CIDRs are required for auth proxy egress")
	}
	podCIDRs, err := normalizedCIDRs(podCIDRs, "Pod")
	if err != nil {
		return nil, nil, err
	}
	serviceCIDRs, err = normalizedCIDRs(serviceCIDRs, "Service")
	if err != nil {
		return nil, nil, err
	}
	return podCIDRs, serviceCIDRs, nil
}

func normalizedCIDRs(cidrs []string, kind string) ([]string, error) {
	unique := make(map[string]struct{}, len(cidrs))
	for _, value := range cidrs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s CIDR %q: %w", kind, value, err)
		}
		unique[prefix.Masked().String()] = struct{}{}
	}
	normalized := make([]string, 0, len(unique))
	for value := range unique {
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func cidrsForFamily(cidrs []string, family string) []string {
	selected := make([]string, 0, len(cidrs))
	for _, value := range cidrs {
		prefix, _ := netip.ParsePrefix(value)
		if (family == "ipv4" && prefix.Addr().Is4()) || (family == "ipv6" && prefix.Addr().Is6()) {
			selected = append(selected, value)
		}
	}
	sort.Strings(selected)
	return selected
}

func networkPolicyPort(protocol corev1.Protocol, port intstr.IntOrString) networkingv1.NetworkPolicyPort {
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &port}
}

func authProxyDNSRules(ctx context.Context, cli client.Client) ([]networkingv1.NetworkPolicyEgressRule, error) {
	service := &corev1.Service{}
	if err := cli.Get(ctx, types.NamespacedName{Namespace: openshiftDNSNamespace, Name: openshiftDNSServiceName}, service); err != nil {
		return nil, fmt.Errorf("get OpenShift DNS Service %s/%s: %w", openshiftDNSNamespace, openshiftDNSServiceName, err)
	}
	if len(service.Spec.Selector) == 0 {
		return nil, fmt.Errorf("OpenShift DNS Service %s/%s has no Pod selector", openshiftDNSNamespace, openshiftDNSServiceName)
	}
	peer := networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": openshiftDNSNamespace}},
		PodSelector:       &metav1.LabelSelector{MatchLabels: service.Spec.Selector},
	}
	var rules []networkingv1.NetworkPolicyEgressRule
	for _, servicePort := range service.Spec.Ports {
		if servicePort.Port != dnsServicePort || (servicePort.Protocol != corev1.ProtocolUDP && servicePort.Protocol != corev1.ProtocolTCP) {
			continue
		}
		targetPort := servicePort.TargetPort
		if targetPort == (intstr.IntOrString{}) {
			targetPort = intstr.FromInt32(servicePort.Port)
		}
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To:    []networkingv1.NetworkPolicyPeer{peer},
			Ports: []networkingv1.NetworkPolicyPort{networkPolicyPort(servicePort.Protocol, targetPort)},
		})
		for _, ip := range serviceClusterIPs(service) {
			prefix, err := exactIPPrefix(ip)
			if err != nil {
				return nil, fmt.Errorf("cluster DNS Service IP: %w", err)
			}
			rules = append(rules, networkingv1.NetworkPolicyEgressRule{
				To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: prefix}}},
				Ports: []networkingv1.NetworkPolicyPort{networkPolicyPort(servicePort.Protocol, intstr.FromInt32(servicePort.Port))},
			})
		}
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("OpenShift DNS Service %s/%s has no UDP or TCP port 53", openshiftDNSNamespace, openshiftDNSServiceName)
	}
	return rules, nil
}

func authProxyAPIRules(ctx context.Context, cli client.Client) ([]networkingv1.NetworkPolicyEgressRule, error) {
	service := &corev1.Service{}
	key := types.NamespacedName{Name: apiServiceName, Namespace: apiServiceNamespace}
	if err := cli.Get(ctx, key, service); err != nil {
		return nil, fmt.Errorf("get Kubernetes API Service: %w", err)
	}
	var rules []networkingv1.NetworkPolicyEgressRule
	for _, servicePort := range service.Spec.Ports {
		if servicePort.Protocol != corev1.ProtocolTCP || servicePort.Port != 443 {
			continue
		}
		for _, ip := range serviceClusterIPs(service) {
			prefix, err := exactIPPrefix(ip)
			if err != nil {
				return nil, fmt.Errorf("kubernetes API Service IP: %w", err)
			}
			rules = append(rules, networkingv1.NetworkPolicyEgressRule{
				To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: prefix}}},
				Ports: []networkingv1.NetworkPolicyPort{networkPolicyPort(corev1.ProtocolTCP, intstr.FromInt32(443))},
			})
		}
	}
	slices := &discoveryv1.EndpointSliceList{}
	if err := cli.List(ctx, slices, client.InNamespace(apiServiceNamespace), client.MatchingLabels{discoveryv1.LabelServiceName: apiServiceName}); err != nil {
		return nil, fmt.Errorf("list Kubernetes API EndpointSlices: %w", err)
	}
	endpointRuleCount := 0
	for _, slice := range slices.Items {
		for _, port := range slice.Ports {
			if port.Port == nil || (port.Protocol != nil && *port.Protocol != corev1.ProtocolTCP) {
				continue
			}
			for _, endpoint := range slice.Endpoints {
				for _, ip := range endpoint.Addresses {
					prefix, err := exactIPPrefix(ip)
					if err != nil {
						return nil, fmt.Errorf("kubernetes API endpoint IP: %w", err)
					}
					rules = append(rules, networkingv1.NetworkPolicyEgressRule{
						To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: prefix}}},
						Ports: []networkingv1.NetworkPolicyPort{networkPolicyPort(corev1.ProtocolTCP, intstr.FromInt32(*port.Port))},
					})
					endpointRuleCount++
				}
			}
		}
	}
	if len(rules) == 0 || endpointRuleCount == 0 {
		return nil, errors.New("kubernetes API Service has no usable IP and EndpointSlice destinations")
	}
	return rules, nil
}

func serviceClusterIPs(service *corev1.Service) []string {
	if len(service.Spec.ClusterIPs) > 0 {
		return service.Spec.ClusterIPs
	}
	if service.Spec.ClusterIP != "" && service.Spec.ClusterIP != corev1.ClusterIPNone {
		return []string{service.Spec.ClusterIP}
	}
	return nil
}

func exactIPPrefix(value string) (string, error) {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return "", fmt.Errorf("parse IP %q: %w", value, err)
	}
	return netip.PrefixFrom(ip, ip.BitLen()).String(), nil
}
