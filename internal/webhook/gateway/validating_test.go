//go:build !nowebhook

package gateway_test

import (
	"net/http"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
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
			name:        "rejects additional ingress without explicit mode on Kubernetes",
			clusterType: cluster.ClusterTypeKubernetes,
			spec: serviceApi.GatewayConfigSpec{
				AdditionalIngresses: serviceApi.AdditionalIngresses{{
					Name:                  "alpha",
					Hostname:              "alpha.example.com",
					IngressControllerName: "shard-alpha",
					RouteLabels:           map[string]string{"example.com/ingress": "alpha"},
				}},
			},
			errorMessages: []string{"additional ingresses require an explicit ingress mode", "received ingressMode=\"\""},
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
			name:        "accepts additional ingress before mode detection on OpenShift",
			clusterType: cluster.ClusterTypeOpenShift,
			spec: serviceApi.GatewayConfigSpec{
				AdditionalIngresses: serviceApi.AdditionalIngresses{{
					Name:                  "alpha",
					Hostname:              "alpha.example.com",
					IngressControllerName: "shard-alpha",
					RouteLabels:           map[string]string{"example.com/ingress": "alpha"},
				}},
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

func TestGatewayConfigValidatorRequestErrors(t *testing.T) {
	g := NewWithT(t)
	sch, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())

	validator := &gatewaywebhook.Validator{
		Decoder: admission.NewDecoder(sch),
		Name:    "test-gatewayconfig-validator",
	}
	gvr := metav1.GroupVersionResource{
		Group:    gvk.GatewayConfig.Group,
		Version:  gvk.GatewayConfig.Version,
		Resource: "gatewayconfigs",
	}

	newRequest := func(operation admissionv1.Operation) admission.Request {
		return envtestutil.NewAdmissionRequest(
			t,
			operation,
			&serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
			},
			gvk.GatewayConfig,
			gvr,
		)
	}

	t.Run("rejects a request when the decoder is not initialized", func(t *testing.T) {
		response := (&gatewaywebhook.Validator{}).Handle(t.Context(), admission.Request{})

		g := NewWithT(t)
		g.Expect(response.Allowed).To(BeFalse())
		g.Expect(response.Result.Code).To(Equal(int32(http.StatusInternalServerError)))
		g.Expect(response.Result.Message).To(ContainSubstring("webhook decoder not initialized"))
	})

	t.Run("rejects a request with an unexpected GVK", func(t *testing.T) {
		request := newRequest(admissionv1.Create)
		request.Kind = metav1.GroupVersionKind{Group: "other.opendatahub.io", Version: "v1", Kind: "Other"}

		response := validator.Handle(t.Context(), request)

		g := NewWithT(t)
		g.Expect(response.Allowed).To(BeFalse())
		g.Expect(response.Result.Code).To(Equal(int32(http.StatusBadRequest)))
		g.Expect(response.Result.Message).To(ContainSubstring("unexpected gvk"))
	})

	t.Run("allows an unsupported operation without decoding the object", func(t *testing.T) {
		request := newRequest(admissionv1.Delete)
		request.Object.Raw = []byte("not-json")

		response := validator.Handle(t.Context(), request)

		g := NewWithT(t)
		g.Expect(response.Allowed).To(BeTrue())
		g.Expect(response.Result.Code).To(Equal(int32(http.StatusOK)))
		g.Expect(response.Result.Message).To(ContainSubstring("Operation DELETE on GatewayConfig allowed"))
	})

	t.Run("rejects a request with an undecodable object", func(t *testing.T) {
		request := newRequest(admissionv1.Create)
		request.Object.Raw = []byte("not-json")

		response := validator.Handle(t.Context(), request)

		g := NewWithT(t)
		g.Expect(response.Allowed).To(BeFalse())
		g.Expect(response.Result.Code).To(Equal(int32(http.StatusBadRequest)))
		g.Expect(response.Result.Message).To(ContainSubstring("failed to decode GatewayConfig"))
	})
}

func TestGatewayConfigAdditionalIngressCrossEntryValidation(t *testing.T) {
	g := NewWithT(t)
	sch, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	validator := &gatewaywebhook.Validator{Decoder: admission.NewDecoder(sch), Name: "test-gatewayconfig-validator"}
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	validIngress := func(name, hostname, labelValue string) serviceApi.AdditionalIngress {
		return serviceApi.AdditionalIngress{
			Name:                  name,
			Hostname:              hostname,
			IngressControllerName: "shard-" + name,
			RouteLabels:           map[string]string{"example.com/ingress": labelValue},
		}
	}
	validOIDC := func(clientID, secretName string) *serviceApi.AdditionalIngressOIDCConfig {
		return &serviceApi.AdditionalIngressOIDCConfig{
			ClientID: clientID,
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  "clientSecret",
			},
		}
	}

	tests := []struct {
		name    string
		spec    serviceApi.GatewayConfigSpec
		message string
	}{
		{
			name: "duplicate hostnames",
			spec: serviceApi.GatewayConfigSpec{
				IngressMode:         serviceApi.IngressModeOcpRoute,
				AdditionalIngresses: serviceApi.AdditionalIngresses{validIngress("alpha", "shared.example.com", "alpha"), validIngress("beta", "shared.example.com", "beta")},
			},
			message: "hostname",
		},
		{
			name: "duplicate route labels",
			spec: serviceApi.GatewayConfigSpec{
				IngressMode:         serviceApi.IngressModeOcpRoute,
				AdditionalIngresses: serviceApi.AdditionalIngresses{validIngress("alpha", "alpha.example.com", "shared"), validIngress("beta", "beta.example.com", "shared")},
			},
			message: "route labels",
		},
		{
			name: "mixed authentication modes",
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
				OIDC: &serviceApi.OIDCConfig{
					ClientID: "default-client",
					ClientSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "default-secret"},
						Key:                  "clientSecret",
					},
				},
				AdditionalIngresses: serviceApi.AdditionalIngresses{validIngress("alpha", "alpha.example.com", "alpha")},
			},
			message: "auth.oidc is required",
		},
		{
			name: "duplicate OIDC client IDs",
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
				OIDC: &serviceApi.OIDCConfig{
					ClientID: "default-client",
					ClientSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "default-secret"},
						Key:                  "clientSecret",
					},
				},
				AdditionalIngresses: func() serviceApi.AdditionalIngresses {
					alpha := validIngress("alpha", "alpha.example.com", "alpha")
					beta := validIngress("beta", "beta.example.com", "beta")
					alpha.Auth.OIDC = validOIDC("shared-client", "alpha-secret")
					beta.Auth.OIDC = validOIDC("shared-client", "beta-secret")
					return serviceApi.AdditionalIngresses{alpha, beta}
				}(),
			},
			message: "clientID",
		},
		{
			name: "duplicate OIDC Secret references",
			spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
				OIDC: &serviceApi.OIDCConfig{
					ClientID: "default-client",
					ClientSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "default-secret"},
						Key:                  "clientSecret",
					},
				},
				AdditionalIngresses: func() serviceApi.AdditionalIngresses {
					alpha := validIngress("alpha", "alpha.example.com", "alpha")
					beta := validIngress("beta", "beta.example.com", "beta")
					alpha.Auth.OIDC = validOIDC("alpha-client", "shared-secret")
					beta.Auth.OIDC = validOIDC("beta-client", "shared-secret")
					beta.Auth.OIDC.ClientSecretRef.Key = "other"
					beta.Auth.OIDC.SecretNamespace = "openshift-ingress"
					return serviceApi.AdditionalIngresses{alpha, beta}
				}(),
			},
			message: "clientSecretRef",
		},
		{
			name: "additional ingresses require OcpRoute",
			spec: serviceApi.GatewayConfigSpec{
				IngressMode:         serviceApi.IngressModeLoadBalancer,
				AdditionalIngresses: serviceApi.AdditionalIngresses{validIngress("alpha", "alpha.example.com", "alpha")},
			},
			message: "require OcpRoute",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := envtestutil.NewAdmissionRequest(t, admissionv1.Create,
				&serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName}, Spec: tc.spec},
				gvk.GatewayConfig,
				metav1.GroupVersionResource{Group: gvk.GatewayConfig.Group, Version: gvk.GatewayConfig.Version, Resource: "gatewayconfigs"})
			response := validator.Handle(t.Context(), request)
			g := NewWithT(t)
			g.Expect(response.Allowed).To(BeFalse())
			g.Expect(response.Result.Message).To(ContainSubstring(tc.message))
		})
	}
}

func TestRegisterWebhooksNilManager(t *testing.T) {
	g := NewWithT(t)
	g.Expect(gatewaywebhook.RegisterWebhooks(nil)).To(MatchError("manager cannot be nil"))
}
