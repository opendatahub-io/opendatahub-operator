//go:build !nowebhook

package gateway_test

import (
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	gatewaywebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestGatewayConfigValidator(t *testing.T) {
	g := NewWithT(t)
	sch, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())

	validator := &gatewaywebhook.Validator{
		Decoder: admission.NewDecoder(sch),
		Name:    "test-gatewayconfig-validator",
	}

	previousClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(previousClusterInfo) })

	testCases := []struct {
		name          string
		clusterType   string
		operation     admissionv1.Operation
		spec          serviceApi.GatewayConfigSpec
		allowed       bool
		errorMessages []string
	}{
		{
			name:        "rejects OpenShift certificate on Kubernetes",
			clusterType: cluster.ClusterTypeKubernetes,
			spec: serviceApi.GatewayConfigSpec{
				Certificate: &infrav1.CertificateSpec{Type: infrav1.OpenshiftDefaultIngress},
			},
			errorMessages: []string{"certificate.type OpenshiftDefaultIngress", "SelfSigned or Provided"},
		},
		{
			name:        "rejects OpenShift route on Kubernetes",
			clusterType: cluster.ClusterTypeKubernetes,
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
			},
			errorMessages: []string{"ingressMode OcpRoute", "LoadBalancer"},
		},
		{
			name:        "rejects both OpenShift-only values on Kubernetes update",
			clusterType: cluster.ClusterTypeKubernetes,
			operation:   admissionv1.Update,
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
				Certificate: &infrav1.CertificateSpec{Type: infrav1.OpenshiftDefaultIngress},
			},
			errorMessages: []string{"certificate.type OpenshiftDefaultIngress", "ingressMode OcpRoute"},
		},
		{
			name:        "accepts Kubernetes values",
			clusterType: cluster.ClusterTypeKubernetes,
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeLoadBalancer,
				Certificate: &infrav1.CertificateSpec{Type: infrav1.SelfSigned},
			},
			allowed: true,
		},
		{
			name:        "accepts a provided certificate on Kubernetes",
			clusterType: cluster.ClusterTypeKubernetes,
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeLoadBalancer,
				Certificate: &infrav1.CertificateSpec{Type: infrav1.Provided, SecretName: "gateway-tls"},
			},
			allowed: true,
		},
		{
			name:        "accepts OpenShift values on OpenShift",
			clusterType: cluster.ClusterTypeOpenShift,
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
				Certificate: &infrav1.CertificateSpec{Type: infrav1.OpenshiftDefaultIngress},
			},
			allowed: true,
		},
		{
			name:        "accepts an unspecified certificate on Kubernetes",
			clusterType: cluster.ClusterTypeKubernetes,
			spec:        serviceApi.GatewayConfigSpec{IngressMode: serviceApi.IngressModeLoadBalancer},
			allowed:     true,
		},
		{
			name:        "accepts an empty certificate on Kubernetes",
			clusterType: cluster.ClusterTypeKubernetes,
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeLoadBalancer,
				Certificate: &infrav1.CertificateSpec{},
			},
			allowed: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			cluster.SetClusterInfo(cluster.ClusterInfo{Type: tc.clusterType})
			operation := tc.operation
			if operation == "" {
				operation = admissionv1.Create
			}

			request := envtestutil.NewAdmissionRequest(
				t,
				operation,
				&serviceApi.GatewayConfig{
					ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
					Spec:       tc.spec,
				},
				gvk.GatewayConfig,
				metav1.GroupVersionResource{
					Group:    gvk.GatewayConfig.Group,
					Version:  gvk.GatewayConfig.Version,
					Resource: "gatewayconfigs",
				},
			)

			response := validator.Handle(t.Context(), request)
			g.Expect(response.Allowed).To(Equal(tc.allowed))
			if !tc.allowed {
				for _, message := range tc.errorMessages {
					g.Expect(response.Result.Message).To(ContainSubstring(message))
				}
			}
		})
	}
}

func TestRegisterWebhooksNilManager(t *testing.T) {
	g := NewWithT(t)
	g.Expect(gatewaywebhook.RegisterWebhooks(nil)).To(MatchError("manager cannot be nil"))
}
