package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type gatewayBackendFixture struct {
	url       string
	routeKey  types.NamespacedName
	routeUID  types.UID
	routeSpec gwapiv1.HTTPRouteSpec
}

func (tc *GatewayTestCtx) deployDefaultGatewayBackendFixture(t *testing.T, requireForwardedToken bool) *gatewayBackendFixture {
	t.Helper()

	name := fmt.Sprintf("gateway-e2e-%x", time.Now().UnixNano())
	path := "/" + name
	namespace := tc.AppsNamespace
	resourceKey := types.NamespacedName{Name: name, Namespace: namespace}
	certPEM, keyPEM := generateDexTLSAssets(t)

	nginxLocation := fmt.Sprintf(`location %s { return 200 "gateway fixture ready\n"; }`, path)
	if requireForwardedToken {
		nginxLocation = fmt.Sprintf(`location %s {
    if ($http_x_forwarded_access_token = "") { return 401; }
    return 200 "forwarded token present\n";
}`, path)
	}
	nginxConfig := fmt.Sprintf(`server {
    listen 8443 ssl;
    server_name _;
    ssl_certificate /etc/nginx/tls/tls.crt;
    ssl_certificate_key /etc/nginx/tls/tls.key;
    %s
}
`, nginxLocation)

	t.Cleanup(func() {
		ctx := context.WithoutCancel(tc.Context())
		objects := []client.Object{
			&gwapiv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name + "-conf", Namespace: namespace}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name + "-tls", Namespace: namespace}},
		}
		for _, object := range objects {
			if err := tc.Client().Delete(ctx, object); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete gateway fixture %T %s: %v", object, name, err)
			}
		}
	})

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-tls", Namespace: namespace},
			Type:       corev1.SecretTypeTLS,
			Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		}),
	)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-conf", Namespace: namespace},
			Data:       map[string]string{"nginx.conf": nginxConfig},
		}),
	)

	fixtureLabels := map[string]string{"app": name}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: new(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: fixtureLabels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: fixtureLabels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "backend",
							Image: echoBackendImage,
							Ports: []corev1.ContainerPort{{Name: "https", ContainerPort: gateway.GatewayHTTPSPort}},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "conf", MountPath: "/etc/nginx/conf.d", ReadOnly: true},
								{Name: "tls", MountPath: "/etc/nginx/tls", ReadOnly: true},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: new(false),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
						}},
						Volumes: []corev1.Volume{
							{
								Name: "conf",
								VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: name + "-conf"},
								}},
							},
							{
								Name: "tls",
								VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
									SecretName: name + "-tls",
								}},
							},
						},
					},
				},
			},
		}),
	)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: corev1.ServiceSpec{
				Selector: fixtureLabels,
				Ports: []corev1.ServicePort{{
					Name:       "https",
					Port:       gateway.GatewayHTTPSPort,
					TargetPort: intstr.FromInt32(gateway.GatewayHTTPSPort),
				}},
			},
		}),
	)

	route := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{ParentRefs: []gwapiv1.ParentReference{{
				Name:      gwapiv1.ObjectName(tc.gatewayName()),
				Namespace: new(gwapiv1.Namespace(tc.gatewayNamespace())),
			}}},
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{
					Type:  new(gwapiv1.PathMatchPathPrefix),
					Value: new(path),
				}}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{
					BackendObjectReference: gwapiv1.BackendObjectReference{
						Name: gwapiv1.ObjectName(name),
						Port: new(gwapiv1.PortNumber(gateway.GatewayHTTPSPort)),
					},
				}}},
			}},
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(route))
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, resourceKey),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
		WithEventuallyTimeout(tc.TestTimeouts.operandReadinessTimeout),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, resourceKey),
		WithCondition(And(
			jq.Match(`.status.parents[0].conditions[] | select(.type == "Accepted") | .status == "True"`),
			jq.Match(`.status.parents[0].conditions[] | select(.type == "ResolvedRefs") | .status == "True"`),
		)),
		WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
	)

	typedRoute := &gwapiv1.HTTPRoute{}
	require.NoError(t, tc.Client().Get(tc.Context(), resourceKey, typedRoute))
	return &gatewayBackendFixture{
		url:       fmt.Sprintf("https://%s%s", tc.getExpectedGatewayHostname(t), path),
		routeKey:  resourceKey,
		routeUID:  typedRoute.UID,
		routeSpec: typedRoute.Spec,
	}
}

func (tc *GatewayTestCtx) assertGatewayBackendFixtureUnchanged(
	ctx context.Context,
	t *testing.T,
	fixture *gatewayBackendFixture,
) {
	t.Helper()
	g := NewWithT(t)
	current := &gwapiv1.HTTPRoute{}
	g.Expect(tc.Client().Get(ctx, fixture.routeKey, current)).To(Succeed())
	g.Expect(current.UID).To(Equal(fixture.routeUID))
	g.Expect(current.Spec).To(Equal(fixture.routeSpec))
}
