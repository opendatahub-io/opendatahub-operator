//go:build integration

package gateway_test

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"

	. "github.com/onsi/gomega"
)

func TestOCPRouteServerTimeoutReconciliation(t *testing.T) {
	for name, setup := range map[string]TestSetup{
		"OAuth": GetOAuthTestSetup(),
		"OIDC":  GetOIDCTestSetup(),
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			defer setup.Setup(t)()
			ctx, cli := setup.TC.Ctx, setup.TC.K8sClient
			key := types.NamespacedName{Name: gateway.DefaultGatewayName, Namespace: gateway.GetGatewayNamespace()}
			const annotation = "haproxy.router.openshift.io/timeout"
			const unrelatedAnnotation = "example.com/preserved"

			route := &routev1.Route{}
			g.Eventually(func() error { return cli.Get(ctx, key, route) }, TestTimeout, TestInterval).Should(Succeed())
			g.Expect(route.Annotations).NotTo(HaveKey(annotation))
			original := route.DeepCopy()

			g.Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := cli.Get(ctx, key, route); err != nil {
					return err
				}
				route.Annotations[unrelatedAnnotation] = "keep"
				return cli.Update(ctx, route)
			})).To(Succeed())

			for _, timeout := range []string{"330s", "10m", ""} {
				spec := setup.Spec
				if timeout != "" {
					spec.OCPRoute = &serviceApi.OCPRouteConfig{ServerTimeout: timeout}
				}
				UpdateGatewayConfig(t, ctx, cli, spec)
				g.Eventually(func(g Gomega) {
					current := &routev1.Route{}
					g.Expect(cli.Get(ctx, key, current)).To(Succeed())
					if timeout == "" {
						g.Expect(current.Annotations).NotTo(HaveKey(annotation))
					} else {
						g.Expect(current.Annotations).To(HaveKeyWithValue(annotation, timeout))
					}
					g.Expect(current.UID).To(Equal(original.UID))
					g.Expect(current.Spec).To(Equal(original.Spec))
					g.Expect(current.Annotations).To(HaveKeyWithValue(unrelatedAnnotation, "keep"))
					g.Expect(current.Annotations).To(HaveKeyWithValue("router.openshift.io/service-ca-certificate", "true"))
				}, TestTimeout, TestInterval).Should(Succeed())
			}
		})
	}
}

func TestOCPRouteServerTimeoutInactiveInLoadBalancerMode(t *testing.T) {
	spec := oauthSpecWithLoadBalancer()
	spec.OCPRoute = &serviceApi.OCPRouteConfig{ServerTimeout: "330s"}
	RunLoadBalancerIngressModeTest(t, OAuthTestEnv, spec)
}
