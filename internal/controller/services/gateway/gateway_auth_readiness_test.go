//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestGatewayAuthenticationReadiness(t *testing.T) {
	original := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	for _, test := range []struct {
		name                          string
		platform                      string
		oidc, certificate, deployment bool
		observed, available           int32
		want                          metav1.ConditionStatus
		additional                    bool
	}{
		{
			name:     "OpenShift OAuth",
			platform: cluster.ClusterTypeOpenShift,
			want:     metav1.ConditionTrue,
		},
		{
			name:     "OpenShift OIDC",
			platform: cluster.ClusterTypeOpenShift,
			oidc:     true,
			want:     metav1.ConditionTrue,
		},
		{
			name:       "OpenShift proxy unavailable",
			platform:   cluster.ClusterTypeOpenShift,
			deployment: true,
			observed:   1,
			want:       metav1.ConditionTrue,
		},
		{
			name:       "OpenShift proxy generation stale",
			platform:   cluster.ClusterTypeOpenShift,
			deployment: true,
			available:  2,
			want:       metav1.ConditionTrue,
		},
		{
			name:       "OpenShift proxy ready",
			platform:   cluster.ClusterTypeOpenShift,
			deployment: true,
			observed:   1,
			available:  2,
			want:       metav1.ConditionTrue,
		},
		{
			name:       "additional OpenShift OAuth proxy missing",
			platform:   cluster.ClusterTypeOpenShift,
			additional: true,
			want:       metav1.ConditionFalse,
		},
		{
			name:       "additional OpenShift OIDC proxy missing",
			platform:   cluster.ClusterTypeOpenShift,
			additional: true,
			oidc:       true,
			want:       metav1.ConditionFalse,
		},
		{
			name:       "additional OpenShift proxy unavailable",
			platform:   cluster.ClusterTypeOpenShift,
			additional: true,
			deployment: true,
			observed:   1,
			want:       metav1.ConditionFalse,
		},
		{
			name:       "additional OpenShift proxy generation stale",
			platform:   cluster.ClusterTypeOpenShift,
			additional: true,
			deployment: true,
			available:  2,
			want:       metav1.ConditionFalse,
		},
		{
			name:       "additional OpenShift proxy ready",
			platform:   cluster.ClusterTypeOpenShift,
			additional: true,
			deployment: true,
			observed:   1,
			available:  2,
			want:       metav1.ConditionTrue,
		},
		{
			name:     "Kubernetes no OIDC",
			platform: cluster.ClusterTypeKubernetes,
			want:     metav1.ConditionTrue,
		},
		{
			name:     "Kubernetes certificate missing",
			platform: cluster.ClusterTypeKubernetes,
			oidc:     true,
			want:     metav1.ConditionFalse,
		},
		{
			name:        "Kubernetes deployment missing",
			platform:    cluster.ClusterTypeKubernetes,
			oidc:        true,
			certificate: true,
			want:        metav1.ConditionFalse,
		},
		{
			name:        "Kubernetes rollout pending",
			platform:    cluster.ClusterTypeKubernetes,
			oidc:        true,
			certificate: true,
			deployment:  true,
			observed:    1,
			want:        metav1.ConditionFalse,
		},
		{
			name:        "Kubernetes generation stale",
			platform:    cluster.ClusterTypeKubernetes,
			oidc:        true,
			certificate: true,
			deployment:  true,
			available:   2,
			want:        metav1.ConditionFalse,
		},
		{
			name:        "Kubernetes ready",
			platform:    cluster.ClusterTypeKubernetes,
			oidc:        true,
			certificate: true,
			deployment:  true,
			observed:    1,
			available:   2,
			want:        metav1.ConditionTrue,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			cluster.SetClusterInfo(cluster.ClusterInfo{
				Type: test.platform,
			})
			ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
			config := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       serviceApi.GatewayConfigName,
					Generation: 1,
				},
				Spec: serviceApi.GatewayConfigSpec{
					Domain: "apps.example.com",
					Certificate: &infrav1.CertificateSpec{
						Type: infrav1.Provided,
					},
				},
			}
			if test.oidc {
				config.Spec.OIDC = &serviceApi.OIDCConfig{}
				ingress.Auth.OIDC = &serviceApi.AdditionalIngressOIDCConfig{}
			}
			target := defaultAuthProxyTarget("apps.example.com", config)
			if test.additional {
				config.Spec.AdditionalIngresses = serviceApi.AdditionalIngresses{ingress}
				config.Status.AdditionalIngresses = buildAdditionalIngressStatuses(config.Spec.AdditionalIngresses, nil, config.Generation)
				target = additionalAuthProxyTarget(ingress, config)
			}
			objects := []client.Object{config, managedGatewayForIngress(GetDefaultGatewayName())}
			if test.certificate {
				cert := &unstructured.Unstructured{}
				cert.SetGroupVersionKind(gvk.CertManagerCertificate)
				cert.SetName(target.TLSName)
				cert.SetNamespace(GetGatewayNamespace())
				cert.SetGeneration(1)
				g.Expect(unstructured.SetNestedSlice(cert.Object, []any{map[string]any{
					"type": "Ready", "status": "True", "observedGeneration": int64(1),
				}}, "status", "conditions")).To(Succeed())
				objects = append(objects, cert, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      target.TLSName,
						Namespace: GetGatewayNamespace(),
					},
					Data: map[string][]byte{corev1.TLSCertKey: []byte("cert"), corev1.TLSPrivateKeyKey: []byte("key")},
				})
			}
			if test.deployment {
				objects = append(objects, &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:       target.ProxyName,
						Namespace:  GetGatewayNamespace(),
						Generation: 1,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: new(int32(2)),
					},
					Status: appsv1.DeploymentStatus{
						ObservedGeneration: int64(test.observed),
						AvailableReplicas:  test.available,
					},
				})
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(objects...),
				fakeclient.WithGVKs(fakeclient.GVKMapping{
					GVK:   gvk.CertManagerCertificate,
					Scope: meta.RESTScopeNamespace,
				}))
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtypes.ReconciliationRequest{
				Instance:   config,
				Client:     cli,
				Conditions: conditionstest.NewManager(config, ReadyConditionType),
			}
			rr.Conditions.MarkUnknown(ReadyConditionType)
			var got *common.Condition
			if test.additional {
				syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)
				g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
				got = conditions.FindStatusCondition(&config.Status.AdditionalIngresses[0], serviceApi.AdditionalIngressAuthenticationReadyConditionType)
			} else {
				g.Expect(syncGatewayConfigStatus(t.Context(), rr)).To(Succeed())
				got = rr.Conditions.GetCondition(ReadyConditionType)
			}
			g.Expect(got).NotTo(BeNil())
			g.Expect(got.Status).To(Equal(test.want), "reason: %s", got.Reason)
			if test.want == metav1.ConditionFalse {
				g.Expect(got.Reason).To(Equal(status.NotReadyReason))
			}
		})
	}
}

func TestAdditionalAuthReadinessPreservesPreparationFailure(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	config.Status.AdditionalIngresses = buildAdditionalIngressStatuses(config.Spec.AdditionalIngresses, nil, config.Generation)
	rr := newGatewayAuthTestRequest(t, config)
	recordAuthResult(rr, additionalAuthProxyTarget(ingress, config), metav1.ConditionFalse,
		additionalIngressReasonInvalidAuthenticationConfig, "OIDC configuration is not supported in integrated OAuth mode")
	syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)
	g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
	condition := conditions.FindStatusCondition(&config.Status.AdditionalIngresses[0], serviceApi.AdditionalIngressAuthenticationReadyConditionType)
	g.Expect(condition).NotTo(BeNil())
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonInvalidAuthenticationConfig))
}

func readyAuthProxyForIngress(name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       KubeAuthProxyName + "-" + name,
			Namespace:  GetGatewayNamespace(),
			Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			AvailableReplicas:  1,
		},
	}
}

func TestAdditionalAuthReadinessRecovers(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	config.Status.AdditionalIngresses = buildAdditionalIngressStatuses(config.Spec.AdditionalIngresses, nil, config.Generation)
	proxy := readyAuthProxyForIngress(ingress.Name)
	proxy.Status.AvailableReplicas = 0
	rr := newGatewayAuthTestRequest(t, config, proxy)
	g.Expect(syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)).To(BeTrue(), "unavailable proxy must request a retry")
	proxy.Status.AvailableReplicas = 1
	g.Expect(rr.Client.Status().Update(t.Context(), proxy)).To(Succeed())
	g.Expect(syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)).To(BeFalse(), "available proxy must recover")
}

func TestAdditionalAuthReadinessIgnoresPersistedFailure(t *testing.T) {
	for _, reason := range []string{additionalIngressReasonInvalidAuthenticationConfig,
		additionalIngressReasonAuthenticationUnavailable, additionalIngressReasonAuthenticationDisabled} {
		t.Run(reason, func(t *testing.T) {
			g := NewWithT(t)
			ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
			config := newGatewayAuthTestConfig(ingress)
			rr := newGatewayAuthTestRequest(t, config, readyAuthProxyForIngress("alpha"))
			g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
			status := additionalIngressStatusByName(config, "alpha")
			setAdditionalIngressCondition(status, config.Generation, serviceApi.AdditionalIngressAuthenticationReadyConditionType,
				metav1.ConditionFalse, reason, "Result from a previous reconciliation")
			g.Expect(syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)).To(BeFalse())
			updateAdditionalGatewaysReadyCondition(rr, config)
			condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressAuthenticationReadyConditionType)
			g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(condition.Reason).To(Equal("Ready"))
		})
	}
}
