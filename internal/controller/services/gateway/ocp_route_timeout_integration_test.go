//go:build integration

package gateway_test

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

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
			g.Eventually(func(g Gomega) {
				g.Expect(cli.Get(ctx, key, route)).To(Succeed())
				g.Expect(route.Annotations).To(HaveKeyWithValue(annotation, "60s"))
			}, TestTimeout, TestInterval).Should(Succeed())
			original := route.DeepCopy()

			g.Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := cli.Get(ctx, key, route); err != nil {
					return err
				}
				route.Annotations[unrelatedAnnotation] = "keep"
				return cli.Update(ctx, route)
			})).To(Succeed())

			for _, timeout := range []string{"330s", "10s", ""} {
				spec := setup.Spec
				if timeout != "" {
					spec.OCPRoute = &serviceApi.OCPRouteConfig{ServerTimeout: timeout}
				}
				UpdateGatewayConfig(t, ctx, cli, spec)
				g.Eventually(func(g Gomega) {
					current := &routev1.Route{}
					g.Expect(cli.Get(ctx, key, current)).To(Succeed())
					if timeout == "" {
						g.Expect(current.Annotations).To(HaveKeyWithValue(annotation, "60s"))
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

func TestOCPRouteServerTimeoutFollowsDefaultIngressController(t *testing.T) {
	for name, setup := range map[string]TestSetup{"OAuth": GetOAuthTestSetup(), "OIDC": GetOIDCTestSetup()} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			defer setup.Setup(t)()
			ctx, cli := setup.TC.Ctx, setup.TC.K8sClient
			ingress := &operatorv1.IngressController{}
			g.Expect(cli.Get(ctx, cluster.IngressControllerName, ingress)).To(Succeed())
			originalTimeout := ingress.Spec.TuningOptions.ServerTimeout
			setRouterTimeout := func(timeout *metav1.Duration) error {
				return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					current := &operatorv1.IngressController{}
					if err := cli.Get(ctx, cluster.IngressControllerName, current); err != nil {
						return err
					}
					current.Spec.TuningOptions.ServerTimeout = timeout
					return cli.Update(ctx, current)
				})
			}
			t.Cleanup(func() { g.Expect(setRouterTimeout(originalTimeout)).To(Succeed()) })
			originalConfig := &serviceApi.GatewayConfig{}
			g.Expect(cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, originalConfig)).To(Succeed())
			key := types.NamespacedName{Name: gateway.DefaultGatewayName, Namespace: gateway.GetGatewayNamespace()}
			for _, tc := range []struct {
				name    string
				timeout *metav1.Duration
				want    string
			}{
				{name: "OpenShift default", want: "60s"},
				{name: "equal minimum", timeout: &metav1.Duration{Duration: time.Minute}},
				{name: "shorter default", timeout: &metav1.Duration{Duration: 15 * time.Second}, want: "60s"},
				{name: "longer default", timeout: &metav1.Duration{Duration: 2 * time.Minute}},
				{name: "removed router timeout", want: "60s"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					g := NewWithT(t)
					g.Expect(setRouterTimeout(tc.timeout)).To(Succeed())
					g.Eventually(func(g Gomega) {
						route := &routev1.Route{}
						g.Expect(cli.Get(ctx, key, route)).To(Succeed())
						if tc.want == "" {
							g.Expect(route.Annotations).NotTo(HaveKey("haproxy.router.openshift.io/timeout"))
						} else {
							g.Expect(route.Annotations).To(HaveKeyWithValue("haproxy.router.openshift.io/timeout", tc.want))
						}
					}, TestTimeout, TestInterval).Should(Succeed())
					current := &serviceApi.GatewayConfig{}
					g.Expect(cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, current)).To(Succeed())
					g.Expect(current.Spec).To(Equal(originalConfig.Spec))
					g.Expect(current.Generation).To(Equal(originalConfig.Generation))
				})
			}
		})
	}
}
