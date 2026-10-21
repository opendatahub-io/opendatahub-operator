//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestGetTemplateDataProviderCAHash covers the provider CA folding in
// getTemplateData: the rollout hash must be extended with the CA bundle
// contents when the referenced secret carries a ca.crt, fail the reconcile
// when the secret is missing or its ca.crt is empty (the rendered Deployment
// keeps the secret volume, so the fallback would roll pods that cannot mount
// a missing secret), and surface real read errors instead of silently
// deploying without the CA.
func TestGetTemplateDataProviderCAHash(t *testing.T) {
	originalClusterInfo := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(originalClusterInfo) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})

	authSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: KubeAuthProxySecretsName, Namespace: GatewayNamespace},
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte("client-id"),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte("client-secret"),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte("cookie-secret"),
		},
	}
	baseHash := calculateAuthConfigHash(authSecret)

	providerCA := []byte("-----BEGIN CERTIFICATE-----\nprovider-ca-bundle\n-----END CERTIFICATE-----\n")
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-ca", Namespace: GatewayNamespace},
		Data:       map[string][]byte{"ca.crt": providerCA},
	}
	emptyCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-ca", Namespace: GatewayNamespace},
		Data:       map[string][]byte{"ca.crt": {}},
	}

	for _, tc := range []struct {
		name        string
		objects     []client.Object
		wantErrText string
		failGet     bool
		wantHash    string
	}{
		{
			name:     "secret with ca.crt extends the rollout hash",
			objects:  []client.Object{authSecret, caSecret},
			wantHash: extendAuthConfigHash(baseHash, providerCA),
		},
		{
			name:        "missing secret fails the reconcile",
			objects:     []client.Object{authSecret},
			wantErrText: "provider CA secret provider-ca not found in namespace",
		},
		{
			name:        "empty ca.crt fails the reconcile",
			objects:     []client.Object{authSecret, emptyCASecret},
			wantErrText: "provider CA secret provider-ca has no ca.crt data",
		},
		{
			name:        "read error surfaces",
			objects:     []client.Object{authSecret},
			wantErrText: "failed to get provider CA secret for hash calculation",
			failGet:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					Domain:               "apps.example.com",
					ProviderCASecretName: "provider-ca",
				},
			}

			scheme, err := testscheme.New()
			g.Expect(err).NotTo(HaveOccurred())

			cli, err := fakeclient.New(
				fakeclient.WithObjects(append(tc.objects, gatewayConfig)...),
				fakeclient.WithScheme(scheme),
				fakeclient.WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, clnt client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if tc.failGet {
							if _, ok := obj.(*corev1.Secret); ok && key.Name == "provider-ca" {
								return k8serr.NewForbidden(schema.GroupResource{Group: "", Resource: "secrets"}, key.Name, nil)
							}
						}
						return clnt.Get(ctx, key, obj, opts...)
					},
				}),
			)
			g.Expect(err).NotTo(HaveOccurred())

			rr := &odhtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   gatewayConfig,
				Conditions: conditions.NewManager(gatewayConfig, ReadyConditionType),
			}

			templateData, err := getTemplateData(ctx, rr)
			if tc.wantErrText != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tc.wantErrText))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(templateData).To(HaveKeyWithValue("AuthConfigHash", tc.wantHash))
			g.Expect(templateData).To(HaveKeyWithValue("ProviderCASecretName", "provider-ca"))
			g.Expect(templateData).To(HaveKeyWithValue("ProviderCASecret", true))
		})
	}
}
