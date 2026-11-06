/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e_test

// Shared shard-topology fixture for the gateway additional-ingress suites
// (RHOAIENG-94376/96913: auth stack, traffic isolation, credential rotation,
// Dex/OCP integration).
//
// newShardIngressFixture creates two additional ingresses backed by real,
// selector-isolated private IngressControllers and wires them into the
// GatewayConfig, giving the gateway specs two independent shards to assert
// against. The gateway is the operator's own service
// (internal/controller/services/gateway), so its behavior is operator-side
// and belongs in this repository's e2e suite.
//
// The notebook-controller assignment matrix that used to live here
// (RHOAIENG-94382) is module behavior — rendered and acted on by the
// Workbenches module operator and the odh-notebook-controller it deploys —
// and is being re-homed in the workbenches-operator repository's e2e suite;
// this repository keeps only the Workbenches CR projection check
// (ValidateIngressProjection in workbenches_test.go).

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

// Fixture-owned OIDC identities for BYOIDC clusters. When the GatewayConfig
// already carries spec.oidc, the admission webhook rejects additional ingresses
// without auth.oidc ("mixed authentication modes"), so the fixture assigns each
// shard a distinct test client ID plus an administrator-style client Secret in
// the gateway namespace. The identities are valid against any issuer: client
// registration only matters at token exchange, which these tests never perform
// (the Dex bootstrap in gateway_dex_ocp_test.go registers matching clients by
// reading auth.oidc and these Secrets).
const fixtureOIDCClientSecretKey = "clientSecret"

// fixtureOIDCClientID returns the test OIDC client ID for an additional ingress.
func fixtureOIDCClientID(ingressName string) string {
	return "e2e-oidc-" + ingressName
}

// fixtureOIDCClientSecretName returns the test client Secret name (gateway
// namespace) carrying the OIDC client secret for an additional ingress.
func fixtureOIDCClientSecretName(ingressName string) string {
	return "e2e-oidc-client-" + ingressName
}

// randomClientSecret returns a random opaque client secret value.
func randomClientSecret(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 24)
	_, err := rand.Read(buf)
	require.NoError(t, err, "generate random client secret")
	return hex.EncodeToString(buf)
}

// injectFixtureOIDCIdentities assigns each ingress a test OIDC identity and
// creates the referenced client Secrets in the gateway namespace before the
// GatewayConfig starts referencing them. It appends the created Secret names to
// secretNames so the registered cleanup can delete them after the restore.
func (tc *GatewayTestCtx) injectFixtureOIDCIdentities(t *testing.T, ingresses []serviceApi.AdditionalIngress, secretNames *[]string) {
	t.Helper()
	namespace := tc.gatewayNamespace()

	for i := range ingresses {
		ingress := &ingresses[i]
		secretName := fixtureOIDCClientSecretName(ingress.Name)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				fixtureOIDCClientSecretKey: randomClientSecret(t),
			},
		}
		tc.EventuallyResourceCreatedOrUpdated(
			WithObjectToCreate(secret),
			WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
		)
		ingress.Auth.OIDC = &serviceApi.AdditionalIngressOIDCConfig{
			ClientID: fixtureOIDCClientID(ingress.Name),
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  fixtureOIDCClientSecretKey,
			},
			SecretNamespace: namespace,
		}
		*secretNames = append(*secretNames, secretName)
	}
}

// deleteFixtureOIDCIdentities removes the fixture-owned client Secrets.
func (tc *GatewayTestCtx) deleteFixtureOIDCIdentities(t *testing.T, secretNames []string) {
	t.Helper()
	cleanupCtx := tc.Context()
	namespace := tc.gatewayNamespace()
	for _, secretName := range secretNames {
		if err := tc.Client().Delete(cleanupCtx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: secretName, Namespace: namespace,
		}}); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete fixture OIDC client secret %s: %v", secretName, err)
		}
	}
}

// shardIngressFixture holds two additional ingresses backed by real,
// selector-isolated private IngressControllers, plus the state required to
// restore the cluster when the owning test finishes.
type shardIngressFixture struct {
	gtc             *GatewayTestCtx
	original        *serviceApi.GatewayConfig
	originalGateway *gwapiv1.Gateway
	ingresses       []serviceApi.AdditionalIngress
	controllers     []types.NamespacedName
}

// newShardIngressFixture creates two additional ingresses backed by real,
// selector-isolated private IngressControllers, wires them into the GatewayConfig,
// and waits until both Gateways are admitted. It deliberately does not wait for
// the Workbenches projection: gateway- and resilience-suite specs assert only on
// the shards' auth stacks and run after the components suite may have set the
// Workbenches module to Removed.
func newShardIngressFixture(t *testing.T, gtc *GatewayTestCtx) *shardIngressFixture {
	t.Helper()
	ctx := gtc.Context()

	original := &serviceApi.GatewayConfig{}
	require.NoError(t, gtc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, original))
	require.NotEmpty(t, original.Status.Domain)
	// The fixture replaces Spec.AdditionalIngresses wholesale, so a cluster
	// with pre-existing additional ingresses would have their gateway and
	// auth-proxy resources torn down and re-added (regenerating their
	// per-ingress credentials). Fail fast instead of mutating them.
	require.Empty(t, original.Spec.AdditionalIngresses,
		"shard fixture requires a GatewayConfig with no pre-existing additional ingresses")
	originalGateway, err := gtc.getGateway(ctx)
	require.NoError(t, err)

	fixture := &shardIngressFixture{
		gtc:             gtc,
		original:        original,
		originalGateway: originalGateway,
	}
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	names := make([]string, 0, 2)
	for _, shard := range []string{"alpha", "beta"} {
		controllerName := fmt.Sprintf("e2e-shard-%s-%s", shard, suffix)
		fixture.ingresses = append(fixture.ingresses, serviceApi.AdditionalIngress{
			Name:                  controllerName,
			Hostname:              controllerName + ".e2e.invalid",
			IngressControllerName: controllerName,
			RouteLabels:           map[string]string{"example.com/ingress": controllerName},
		})
		fixture.controllers = append(fixture.controllers, types.NamespacedName{
			Name: controllerName, Namespace: cluster.IngressControllerName.Namespace,
		})
		names = append(names, controllerName)
	}

	// Register cleanups before creating anything, so an aborted setup still
	// restores state; evidence is dumped before the restore mutates it.
	// The identity-Secret cleanup is registered first (runs last, LIFO) so the
	// Secrets are deleted only after the GatewayConfig restore no longer
	// references them.
	oidcSecretNames := make([]string, 0, len(fixture.ingresses))
	t.Cleanup(func() { gtc.deleteFixtureOIDCIdentities(t, oidcSecretNames) })
	t.Cleanup(func() {
		restoreAdditionalIngressGatewayTest(t, gtc, fixture.original, fixture.originalGateway, names, fixture.controllers...)
	})
	t.Cleanup(func() { gtc.logAdditionalIngressEvidence(t, fixture.controllers...) })

	replicas := int32(1)
	for i, ingress := range fixture.ingresses {
		require.NoError(t, gtc.Client().Create(ctx, &operatorv1.IngressController{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fixture.controllers[i].Name,
				Namespace: fixture.controllers[i].Namespace,
			},
			Spec: operatorv1.IngressControllerSpec{
				Domain:   ingress.Hostname,
				Replicas: &replicas,
				EndpointPublishingStrategy: &operatorv1.EndpointPublishingStrategy{
					Type: operatorv1.PrivateStrategyType,
				},
				RouteSelector: &metav1.LabelSelector{MatchLabels: ingress.RouteLabels},
			},
		}), "create temporary private IngressController %s", fixture.controllers[i].Name)
	}

	// On BYOIDC clusters (GatewayConfig already carries spec.oidc), assign the
	// shards their test OIDC identities before the GatewayConfig references
	// them; on IntegratedOAuth clusters the ingresses stay plain and the
	// operator registers data-science-* OAuthClients instead.
	if original.Spec.OIDC != nil {
		gtc.injectFixtureOIDCIdentities(t, fixture.ingresses, &oidcSecretNames)
	}

	g := NewWithT(t)
	g.Eventually(func() error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := gtc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = fixture.ingresses
			return gtc.Client().Update(ctx, current)
		})
	}).Should(Succeed())

	for _, ingress := range fixture.ingresses {
		gtc.validateAdditionalGatewayAndRoute(t, ingress)
		gtc.validateAdditionalGatewayStatus(t, len(fixture.ingresses), ingress.Name, ingress.Hostname)
		validateAdditionalIngressRouteAdmissionStatus(t, gtc,
			types.NamespacedName{Name: ingress.Name, Namespace: gtc.gatewayNamespace()},
			ingress.Name, ingress.Hostname, ingress.IngressControllerName)
	}

	return fixture
}
