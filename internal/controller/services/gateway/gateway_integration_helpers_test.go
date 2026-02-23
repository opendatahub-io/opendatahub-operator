//go:build integration

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

// Package gateway_test provides integration tests for the gateway controller
// (GatewayConfig reconciliation, OAuth and OIDC auth modes).
//
// What is tested: creation and shape of GatewayClass, Gateway, HTTPRoute,
// Service, Deployment, HPA, EnvoyFilter, OCP Route, NetworkPolicy,
// DestinationRule; auth-proxy args and secrets; spec mutation (cookie, subdomain,
// OIDC issuer); status conditions and domain.
//
// Prerequisites: run with -tags=integration. TestMain starts two envtest
// environments (OAuth and OIDC) once; tests use OAuthTestEnv / OIDCTestEnv
// and do not start envtest per test.
//
// Structure: tests use TestSetup (env + spec + optional setup func). Helpers
// like RunGatewayClassCreationTest(t, setup) implement the assertions; OAuth/OIDC
// test files call them with GetOAuthTestSetup() or GetOIDCTestSetup(). To add a
// test: add a RunFooTest in the helpers and call it from both OAuth and OIDC
// files (or one if auth-specific).
package gateway_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

const (
	TestTimeout  = 60 * time.Second
	TestInterval = 500 * time.Millisecond

	controllerUserName      = "gateway-controller"
	defaultCookieExpireArg  = "--cookie-expire=24h0m0s"
	defaultCookieRefreshArg = "--cookie-refresh=1h0m0s"
	dsciName                = "default-dsci"
	updatedCookieExpireArg  = "--cookie-expire=48h0m0s"
	updatedCookieRefreshArg = "--cookie-refresh=2h0m0s"
)

const (
	OAuthClusterDomain = "apps.oauth-test.example.com"
	OIDCClusterDomain  = "apps.oidc-test.example.com"
)

const (
	OIDCIssuerURL  = "https://keycloak.example.com/realms/test"
	OIDCClientID   = "test-oidc-client"
	OIDCSecretName = "oidc-client-secret"
	OIDCSecretKey  = "client-secret"
)

var (
	OAuthTestEnv *TestEnvContext
	OIDCTestEnv  *TestEnvContext
)

// DefaultGatewayHost returns the default gateway hostname (default subdomain + domain).
func DefaultGatewayHost(domain string) string {
	return gateway.DefaultGatewaySubdomain + "." + domain
}

// LegacyGatewayHost returns the legacy gateway hostname (legacy subdomain + domain).
func LegacyGatewayHost(domain string) string {
	return gateway.LegacyGatewaySubdomain + "." + domain
}

// HostForSubdomain returns hostname for a custom subdomain (subdomain + "." + domain).
func HostForSubdomain(subdomain, domain string) string {
	return subdomain + "." + domain
}

// OAuthRedirectURI returns the OAuth redirect URI for the given hostname (used to assert OAuthClient.RedirectURIs).
func OAuthRedirectURI(hostname string) string {
	return "https://" + hostname + gateway.AuthProxyOAuth2Path + "/callback"
}

// SpecMutationCookieConfig returns the cookie config used in spec-mutation tests (48h expire, 2h refresh).
func SpecMutationCookieConfig() serviceApi.CookieConfig {
	return serviceApi.CookieConfig{
		Expire:  metav1.Duration{Duration: 48 * time.Hour},
		Refresh: metav1.Duration{Duration: 2 * time.Hour},
	}
}

// getAuthProxyDeployment fetches the kube-auth-proxy Deployment from the gateway namespace.
// Used by tests that assert on deployment args/volumes.
func getAuthProxyDeployment(ctx context.Context, cli client.Client) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	err := cli.Get(ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxyName,
		Namespace: gateway.GatewayNamespace,
	}, deployment)
	return deployment, err
}

// assertOwnedByGatewayConfig asserts that the object has an owner reference to the GatewayConfig singleton.
// Resources created by the gateway controller must be owned by GatewayConfig so they are garbage-collected when GatewayConfig is deleted.
func assertOwnedByGatewayConfig(g *WithT, obj client.Object) {
	refs := obj.GetOwnerReferences()
	g.Expect(refs).NotTo(BeEmpty(), "resource %s/%s should have an owner reference to GatewayConfig", obj.GetNamespace(), obj.GetName())
	var found bool
	for _, ref := range refs {
		if ref.Kind == serviceApi.GatewayConfigKind && ref.Name == serviceApi.GatewayConfigName {
			g.Expect(ref.APIVersion).To(Equal(gvk.GatewayConfig.GroupVersion().String()), "owner ref APIVersion should be GatewayConfig group/version")
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), "resource should be owned by GatewayConfig %s", serviceApi.GatewayConfigName)
}

// TestEnvContext holds envtest context, cancel, env, client, and scheme for one auth mode.
// Shared across tests; created in TestMain.
type TestEnvContext struct {
	Ctx       context.Context
	Cancel    context.CancelFunc
	TestEnv   *envtest.Environment
	K8sClient client.Client
	Scheme    *runtime.Scheme
}

func readClusterRolesFromFile(filePath string) ([]*rbacv1.ClusterRole, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	var roles []*rbacv1.ClusterRole
	reader := k8syaml.NewYAMLReader(bufio.NewReader(file))

	for {
		data, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read YAML document: %w", err)
		}

		// Skip empty documents
		if len(data) == 0 {
			continue
		}

		role := &rbacv1.ClusterRole{}
		if err := k8syaml.Unmarshal(data, role); err != nil {
			// Skip non-ClusterRole documents
			continue
		}

		// Only add if it's actually a ClusterRole
		if role.Kind == "ClusterRole" && role.APIVersion == "rbac.authorization.k8s.io/v1" {
			roles = append(roles, role)
		}
	}

	return roles, nil
}

func setupRBACForUser(ctx context.Context, cli client.Client, rootPath string, userName string) error {
	roleYamlPath := filepath.Join(rootPath, "config", "rbac", "role.yaml")
	roles, err := readClusterRolesFromFile(roleYamlPath)
	if err != nil {
		return fmt.Errorf("failed to read role.yaml: %w", err)
	}

	if len(roles) == 0 {
		return fmt.Errorf("no ClusterRole found in %s", roleYamlPath)
	}

	for _, role := range roles {
		if err := cli.Create(ctx, role); err != nil {
			if err = client.IgnoreAlreadyExists(err); err != nil {
				return fmt.Errorf("failed to create ClusterRole %s: %w", role.Name, err)
			}
		}

		// Create ClusterRoleBinding for the user
		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s-binding", role.Name, userName),
			},
			Subjects: []rbacv1.Subject{
				{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "User",
					Name:     userName,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     role.Name,
			},
		}
		if err := cli.Create(ctx, binding); err != nil {
			if err = client.IgnoreAlreadyExists(err); err != nil {
				return fmt.Errorf("failed to create ClusterRoleBinding: %w", err)
			}
		}
	}

	return nil
}

func createControllerUser(testEnv *envtest.Environment, adminCfg *rest.Config) (*rest.Config, error) {
	user, err := testEnv.AddUser(
		envtest.User{Name: controllerUserName, Groups: []string{"system:authenticated"}},
		adminCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add user: %w", err)
	}
	return user.Config(), nil
}

// SetupTestEnvForMain builds and starts envtest (CRDs, RBAC, controller) for the given auth mode and cluster domain.
// Called from TestMain for OAuth and OIDC.
func SetupTestEnvForMain(authMode string, clusterDomain string) *TestEnvContext {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	rootPath, err := envtestutil.FindProjectRoot()
	if err != nil {
		panic(fmt.Sprintf("Failed to find project root: %v", err))
	}

	scheme, err := testscheme.New()
	if err != nil {
		panic(fmt.Sprintf("Failed to create scheme: %v", err))
	}
	utilruntime.Must(configv1.Install(scheme))

	testEnv := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: scheme,
			Paths: []string{
				filepath.Join(rootPath, "config", "crd", "bases"),
				filepath.Join(rootPath, "config", "crd", "external"),
			},
			CRDs:               getIstioCRDs(),
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		panic(fmt.Sprintf("Failed to start envtest: %v", err))
	}

	ctx, cancel := context.WithCancel(context.Background())

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to create k8s client: %v", err))
	}

	controllerCfg, err := createControllerUser(testEnv, cfg)
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to create controller user: %v", err))
	}

	if err := setupRBACForUser(ctx, k8sClient, rootPath, controllerUserName); err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to setup RBAC: %v", err))
	}

	setupClusterPrerequisitesForMain(ctx, k8sClient, authMode, clusterDomain)

	_ = os.Setenv("OPERATOR_NAMESPACE", gateway.GatewayNamespace)
	if os.Getenv("ODH_PLATFORM_TYPE") == "" {
		_ = os.Setenv("ODH_PLATFORM_TYPE", "OpenDataHub")
	}

	// Initialize cluster config so cluster.GetOperatorNamespace() works (e.g. for GC action).
	// Ignore Init error: operator namespace is set above; other steps may fail in envtest (e.g. no ClusterVersion).
	_ = cluster.Init(ctx, k8sClient)

	// Manager with production-like cache. Do not add GatewayConfig to DisableFor (controller must receive watch events on spec updates).
	skipNameValidation := true
	ctrlMgr, err := ctrl.NewManager(controllerCfg, ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,
		Metrics: ctrlmetrics.Options{
			BindAddress: "0",
		},
		Controller: ctrlconfig.Controller{
			SkipNameValidation: &skipNameValidation,
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					resources.GvkToUnstructured(gvk.OpenshiftIngress),
					resources.GvkToUnstructured(gvk.KubernetesGateway),
				},
				Unstructured: true,
			},
		},
	})
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to create manager: %v", err))
	}
	mgr := manager.New(ctrlMgr)

	handler := &gateway.ServiceHandler{}
	if err := handler.NewReconciler(ctx, mgr); err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to setup controller: %v", err))
	}

	tc := &TestEnvContext{
		Ctx:       ctx,
		Cancel:    cancel,
		TestEnv:   testEnv,
		K8sClient: k8sClient,
		Scheme:    scheme,
	}
	go func() {
		_ = mgr.Start(ctx)
	}()
	return tc
}

func setupClusterPrerequisitesForMain(ctx context.Context, cli client.Client, authMode, clusterDomain string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: gateway.GatewayNamespace,
		},
	}
	if err := cli.Create(ctx, ns); err != nil {
		panic(fmt.Sprintf("Failed to create namespace: %v", err))
	}

	ingress := &configv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: configv1.IngressSpec{
			Domain: clusterDomain,
		},
	}
	if err := cli.Create(ctx, ingress); err != nil {
		panic(fmt.Sprintf("Failed to create Ingress: %v", err))
	}

	auth := &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{
			Name: cluster.ClusterAuthenticationObj,
		},
		Spec: configv1.AuthenticationSpec{
			Type: configv1.AuthenticationType(authMode),
		},
	}
	if err := cli.Create(ctx, auth); err != nil {
		panic(fmt.Sprintf("Failed to create Authentication: %v", err))
	}

	appsNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cluster.DefaultNotebooksNamespaceODH,
		},
	}
	if err := cli.Create(ctx, appsNs); err != nil {
		panic(fmt.Sprintf("Failed to create applications namespace: %v", err))
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: dsciName,
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: cluster.DefaultNotebooksNamespaceODH,
		},
	}
	if err := cli.Create(ctx, dsci); err != nil {
		panic(fmt.Sprintf("Failed to create DSCInitialization: %v", err))
	}
	ensureLoadBalancerPrerequisites(ctx, cli)
}

// ensureLoadBalancerPrerequisites creates namespaces, default IngressController, and router-certs-default
// secret so the controller can propagate the default ingress cert in LoadBalancer mode.
func ensureLoadBalancerPrerequisites(ctx context.Context, cli client.Client) {
	nsIO := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-ingress-operator"}}
	if err := cli.Create(ctx, nsIO); err != nil && !k8serr.IsAlreadyExists(err) {
		panic(fmt.Sprintf("Failed to create openshift-ingress-operator namespace: %v", err))
	}
	nsI := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.IngressNamespace}}
	if err := cli.Create(ctx, nsI); err != nil && !k8serr.IsAlreadyExists(err) {
		panic(fmt.Sprintf("Failed to create %s namespace: %v", cluster.IngressNamespace, err))
	}
	ic := &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "openshift-ingress-operator",
		},
		Spec: operatorv1.IngressControllerSpec{},
	}
	if err := cli.Create(ctx, ic); err != nil && !k8serr.IsAlreadyExists(err) {
		panic(fmt.Sprintf("Failed to create IngressController: %v", err))
	}
	secret, err := cluster.GenerateSelfSignedCertificateAsSecret("router-certs-default", "test.example.com", cluster.IngressNamespace)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate router-certs-default secret: %v", err))
	}
	if err := cli.Create(ctx, secret); err != nil && !k8serr.IsAlreadyExists(err) {
		panic(fmt.Sprintf("Failed to create router-certs-default secret: %v", err))
	}
}

func getIstioCRDs() []*apiextensionsv1.CustomResourceDefinition {
	preserveUnknown := true

	// EnvoyFilter CRD
	envoyFilterCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "envoyfilters.networking.istio.io",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "networking.istio.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "EnvoyFilter",
				ListKind: "EnvoyFilterList",
				Plural:   "envoyfilters",
				Singular: "envoyfilter",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha3",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: &preserveUnknown,
						},
					},
				},
			},
		},
	}

	destinationRuleCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "destinationrules.networking.istio.io",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "networking.istio.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "DestinationRule",
				ListKind: "DestinationRuleList",
				Plural:   "destinationrules",
				Singular: "destinationrule",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: &preserveUnknown,
						},
					},
				},
			},
		},
	}

	return []*apiextensionsv1.CustomResourceDefinition{envoyFilterCRD, destinationRuleCRD}
}

// deleteGatewayConfigDependents deletes resources that would be cascade-deleted by Kubernetes GC in a real cluster
// (Gateway, gateway OCP Routes, kube-auth-proxy Deployment). Envtest does not run kube-controller-manager, so GC
// never runs; this cleanup runs from DeleteGatewayConfig after GatewayConfig is gone. Extend this function if more
// dependent resource types need explicit deletion in envtest.
func deleteGatewayConfigDependents(t *testing.T, ctx context.Context, cli client.Client) {
	t.Helper()
	g := NewWithT(t)
	ns := gateway.GatewayNamespace

	// Delete Gateway if it exists.
	gw := &gwapiv1.Gateway{}
	if err := cli.Get(ctx, types.NamespacedName{
		Name:      gateway.DefaultGatewayName,
		Namespace: ns,
	}, gw); err == nil {
		if err := cli.Delete(ctx, gw); err != nil {
			t.Fatalf("Failed to delete Gateway: %v", err)
		}
		g.Eventually(func() bool {
			err := cli.Get(ctx, types.NamespacedName{Name: gateway.DefaultGatewayName, Namespace: ns}, &gwapiv1.Gateway{})
			return err != nil && k8serr.IsNotFound(err)
		}, TestTimeout, TestInterval).Should(BeTrue(), "Gateway should be deleted")
	} else if !k8serr.IsNotFound(err) {
		t.Fatalf("Failed to get Gateway: %v", err)
	}

	// Delete gateway OCP Routes if they exist. We do not wait for Routes to be gone—envtest deletion can be slow;
	// tests that need "no Route" (e.g. LoadBalancer) use their own Eventually.
	for _, name := range []string{gateway.DefaultGatewayName, gateway.DefaultGatewayName + "-legacy-redirect"} {
		route := &routev1.Route{}
		if err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, route); err == nil {
			_ = cli.Delete(ctx, route)
		}
	}

	// Delete kube-auth-proxy Deployment if it exists (no wait).
	dep := &appsv1.Deployment{}
	if err := cli.Get(ctx, types.NamespacedName{Name: gateway.KubeAuthProxyName, Namespace: ns}, dep); err == nil {
		_ = cli.Delete(ctx, dep)
	} else if !k8serr.IsNotFound(err) {
		t.Fatalf("Failed to get Deployment %s: %v", gateway.KubeAuthProxyName, err)
	}
}

// CreateGatewayConfig ensures no existing GatewayConfig, then creates one with the given spec. Waits until the object exists.
// Callers should defer DeleteGatewayConfig so dependent resources (Gateway, Routes) are cleaned up after each test.
func CreateGatewayConfig(t *testing.T, ctx context.Context, cli client.Client, spec serviceApi.GatewayConfigSpec) {
	t.Helper()

	g := NewWithT(t)
	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc)
		if err != nil && !k8serr.IsNotFound(err) {
			t.Fatalf("Failed to check GatewayConfig deletion: %v", err)
		}
		return k8serr.IsNotFound(err)
	}, TestTimeout, TestInterval).Should(BeTrue(), "Previous GatewayConfig should be deleted before creating a new one")

	gc := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
		Spec: spec,
	}
	if err := cli.Create(ctx, gc); err != nil {
		t.Fatalf("Failed to create GatewayConfig: %v", err)
	}
	g.Eventually(func() bool {
		created := &serviceApi.GatewayConfig{}
		err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, created)
		return err == nil
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig should exist after create")
}

// DeleteGatewayConfig deletes the GatewayConfig, waits until it is gone, then deletes dependent resources
// (Gateway, OCP Routes, kube-auth-proxy Deployment) that envtest's lack of GC would otherwise leave behind. See deleteGatewayConfigDependents.
func DeleteGatewayConfig(t *testing.T, ctx context.Context, cli client.Client) {
	t.Helper()
	gc := &serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err == nil {
		if err := cli.Delete(ctx, gc); err != nil {
			t.Logf("Warning: failed to delete GatewayConfig: %v", err)
		}
	}
	g := NewWithT(t)
	g.Eventually(func() bool {
		gone := &serviceApi.GatewayConfig{}
		err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gone)
		return err != nil && k8serr.IsNotFound(err)
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig should not exist after delete")

	deleteGatewayConfigDependents(t, ctx, cli)
}

// UpdateGatewayConfig updates the existing GatewayConfig spec in place.
func UpdateGatewayConfig(t *testing.T, ctx context.Context, cli client.Client, spec serviceApi.GatewayConfigSpec) {
	t.Helper()
	gc := &serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
		t.Fatalf("Failed to get GatewayConfig: %v", err)
	}
	gc.Spec = spec
	if err := cli.Update(ctx, gc); err != nil {
		t.Fatalf("Failed to update GatewayConfig: %v", err)
	}
}

// TestSetup is test configuration for a single test: env context, GatewayConfig spec, and optional setup func (e.g. create OIDC secret).
// Passed to Run*Test helpers. Create with GetOAuthTestSetup / GetOIDCTestSetup. To add a test: add RunFooTest(t, setup) below and call it from gateway_oauth_integration_test.go and gateway_oidc_integration_test.go.
type TestSetup struct {
	TC        *TestEnvContext
	Spec      serviceApi.GatewayConfigSpec
	SetupFunc func(t *testing.T, tc *TestEnvContext)
}

func (s TestSetup) Setup(t *testing.T) func() {
	t.Helper()
	if s.SetupFunc != nil {
		s.SetupFunc(t, s.TC)
	}
	CreateGatewayConfig(t, s.TC.Ctx, s.TC.K8sClient, s.Spec)
	return func() {
		DeleteGatewayConfig(t, s.TC.Ctx, s.TC.K8sClient)
	}
}

// RunGatewayClassCreationTest validates that the GatewayClass is created with the expected controller name.
func RunGatewayClassCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		gc := &gwapiv1.GatewayClass{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: gateway.GatewayClassName}, gc)
	}, TestTimeout, TestInterval).Should(Succeed())

	gc := &gwapiv1.GatewayClass{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: gateway.GatewayClassName}, gc)).To(Succeed())
	assertOwnedByGatewayConfig(g, gc)
	g.Expect(string(gc.Spec.ControllerName)).To(Equal(gateway.GatewayControllerName))
}

// RunGatewayCreationTest validates that the Gateway is created with the correct class and HTTPS listener.
func RunGatewayCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		gw := &gwapiv1.Gateway{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}, gw)
	}, TestTimeout, TestInterval).Should(Succeed())

	gw := &gwapiv1.Gateway{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.DefaultGatewayName,
		Namespace: gateway.GatewayNamespace,
	}, gw)).To(Succeed())
	assertOwnedByGatewayConfig(g, gw)
	g.Expect(string(gw.Spec.GatewayClassName)).To(Equal(gateway.GatewayClassName))

	hasHTTPSListener := false
	for _, listener := range gw.Spec.Listeners {
		if listener.Name == "https" {
			hasHTTPSListener = true
			g.Expect(listener.Port).To(Equal(gwapiv1.PortNumber(gateway.StandardHTTPSPort)))
			g.Expect(listener.Protocol).To(Equal(gwapiv1.HTTPSProtocolType))
			break
		}
	}
	g.Expect(hasHTTPSListener).To(BeTrue(), "Gateway should have HTTPS listener")
}

// RunHTTPRouteCreationTest validates that the HTTPRoute exists with the expected parentRef, path match, and backend.
func RunHTTPRouteCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		route := &gwapiv1.HTTPRoute{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.OAuthCallbackRouteName,
			Namespace: gateway.GatewayNamespace,
		}, route)
	}, TestTimeout, TestInterval).Should(Succeed())

	route := &gwapiv1.HTTPRoute{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.OAuthCallbackRouteName,
		Namespace: gateway.GatewayNamespace,
	}, route)).To(Succeed())
	assertOwnedByGatewayConfig(g, route)

	g.Expect(route.Spec.ParentRefs).NotTo(BeEmpty())
	g.Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal(gateway.DefaultGatewayName))

	g.Expect(route.Spec.Rules).NotTo(BeEmpty())
	g.Expect(route.Spec.Rules[0].Matches).NotTo(BeEmpty())
	g.Expect(route.Spec.Rules[0].Matches[0].Path.Type).NotTo(BeNil())
	g.Expect(string(*route.Spec.Rules[0].Matches[0].Path.Type)).To(Equal("PathPrefix"))
	g.Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal(gateway.AuthProxyOAuth2Path))

	g.Expect(route.Spec.Rules[0].BackendRefs).NotTo(BeEmpty())
	g.Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(gateway.KubeAuthProxyName))
	g.Expect(int(*route.Spec.Rules[0].BackendRefs[0].Port)).To(Equal(gateway.GatewayHTTPSPort))
}

// RunServiceCreationTest validates that the Service is created with the expected selector, ports, and annotations.
func RunServiceCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		svc := &corev1.Service{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, svc)
	}, TestTimeout, TestInterval).Should(Succeed())

	svc := &corev1.Service{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxyName,
		Namespace: gateway.GatewayNamespace,
	}, svc)).To(Succeed())
	assertOwnedByGatewayConfig(g, svc)

	// Verify selector
	g.Expect(svc.Spec.Selector).To(HaveKeyWithValue("app", gateway.KubeAuthProxyName))

	// Verify ports
	g.Expect(svc.Spec.Ports).To(HaveLen(2))

	hasHTTPS := false
	hasMetrics := false
	for _, port := range svc.Spec.Ports {
		if port.Name == "https" {
			hasHTTPS = true
			g.Expect(port.Port).To(Equal(int32(gateway.GatewayHTTPSPort)))
			g.Expect(port.TargetPort.IntVal).To(Equal(int32(gateway.GatewayHTTPSPort)))
		}
		if port.Name == "metrics" {
			hasMetrics = true
			g.Expect(port.Port).To(Equal(int32(gateway.AuthProxyMetricsPort)))
		}
	}
	g.Expect(hasHTTPS).To(BeTrue(), "Service should have HTTPS port")
	g.Expect(hasMetrics).To(BeTrue(), "Service should have metrics port")

	// Verify service-ca annotation for TLS
	g.Expect(svc.Annotations).To(HaveKeyWithValue(
		"service.beta.openshift.io/serving-cert-secret-name",
		gateway.KubeAuthProxyTLSName,
	))
}

// RunAuthProxySecretCreationTest validates that the kube-auth-proxy-creds secret exists with required keys; if expectedClientID is set, asserts client ID (e.g. OIDC).
func RunAuthProxySecretCreationTest(t *testing.T, setup TestSetup, expectedClientID string) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		secret := &corev1.Secret{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxySecretsName,
			Namespace: gateway.GatewayNamespace,
		}, secret)
	}, TestTimeout, TestInterval).Should(Succeed())

	secret := &corev1.Secret{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxySecretsName,
		Namespace: gateway.GatewayNamespace,
	}, secret)).To(Succeed())
	assertOwnedByGatewayConfig(g, secret)

	g.Expect(secret.Data).To(HaveKey(gateway.EnvClientID))
	g.Expect(secret.Data).To(HaveKey(gateway.EnvClientSecret))
	g.Expect(secret.Data).To(HaveKey(gateway.EnvCookieSecret))
	g.Expect(secret.Data[gateway.EnvClientID]).NotTo(BeEmpty())
	g.Expect(secret.Data[gateway.EnvClientSecret]).NotTo(BeEmpty())
	g.Expect(secret.Data[gateway.EnvCookieSecret]).NotTo(BeEmpty())
	if expectedClientID != "" {
		g.Expect(string(secret.Data[gateway.EnvClientID])).To(Equal(expectedClientID))
	}
}

// RunHPACreationTest validates that the HPA is created with the correct scaling configuration.
func RunHPACreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, hpa)
	}, TestTimeout, TestInterval).Should(Succeed())

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxyName,
		Namespace: gateway.GatewayNamespace,
	}, hpa)).To(Succeed())
	assertOwnedByGatewayConfig(g, hpa)

	// Verify scale target
	g.Expect(hpa.Spec.ScaleTargetRef.APIVersion).To(Equal("apps/v1"))
	g.Expect(hpa.Spec.ScaleTargetRef.Kind).To(Equal("Deployment"))
	g.Expect(hpa.Spec.ScaleTargetRef.Name).To(Equal(gateway.KubeAuthProxyName))

	// Verify replica bounds
	g.Expect(*hpa.Spec.MinReplicas).To(Equal(int32(2)))
	g.Expect(hpa.Spec.MaxReplicas).To(Equal(int32(10)))

	// Verify behavior
	g.Expect(hpa.Spec.Behavior).NotTo(BeNil())
	g.Expect(hpa.Spec.Behavior.ScaleDown).NotTo(BeNil())
	g.Expect(*hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds).To(Equal(int32(300)))
	g.Expect(hpa.Spec.Behavior.ScaleUp).NotTo(BeNil())
	g.Expect(*hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds).To(Equal(int32(0)))
	g.Expect(*hpa.Spec.Behavior.ScaleUp.SelectPolicy).To(Equal(autoscalingv2.MaxChangePolicySelect))

	// Verify metrics
	g.Expect(hpa.Spec.Metrics).To(HaveLen(1))
	g.Expect(hpa.Spec.Metrics[0].Type).To(Equal(autoscalingv2.ResourceMetricSourceType))
	g.Expect(hpa.Spec.Metrics[0].Resource.Name).To(Equal(corev1.ResourceCPU))
	g.Expect(hpa.Spec.Metrics[0].Resource.Target.Type).To(Equal(autoscalingv2.UtilizationMetricType))
	g.Expect(*hpa.Spec.Metrics[0].Resource.Target.AverageUtilization).To(Equal(int32(70)))
}

// RunEnvoyFilterCreationTest validates that the EnvoyFilter is created with workload selector and configPatches.
func RunEnvoyFilterCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		ef := &unstructured.Unstructured{}
		ef.SetGroupVersionKind(gvk.EnvoyFilter)
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.AuthnFilterName,
			Namespace: gateway.GatewayNamespace,
		}, ef)
	}, TestTimeout, TestInterval).Should(Succeed())

	ef := &unstructured.Unstructured{}
	ef.SetGroupVersionKind(gvk.EnvoyFilter)
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.AuthnFilterName,
		Namespace: gateway.GatewayNamespace,
	}, ef)).To(Succeed())
	assertOwnedByGatewayConfig(g, ef)

	selector, found, err := unstructured.NestedStringMap(ef.Object, "spec", "workloadSelector", "labels")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(selector).To(HaveKeyWithValue("gateway.networking.k8s.io/gateway-name", gateway.DefaultGatewayName))

	patches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(len(patches)).To(BeNumerically(">=", 2))
}

// RunEnvoyFilterExtAuthzConfigurationTest validates EnvoyFilter ext_authz config (timeout, http_service). Setup must use a spec with AuthProxyTimeout set (e.g. 45s).
func RunEnvoyFilterExtAuthzConfigurationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	var ef *unstructured.Unstructured
	g.Eventually(func() string {
		ef = &unstructured.Unstructured{}
		ef.SetGroupVersionKind(gvk.EnvoyFilter)
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.AuthnFilterName,
			Namespace: gateway.GatewayNamespace,
		}, ef); err != nil {
			return ""
		}
		patches, _, _ := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
		for _, p := range patches {
			patch, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			patchValue, _, _ := unstructured.NestedMap(patch, "patch", "value")
			if patchValue == nil {
				continue
			}
			name, _, _ := unstructured.NestedString(patchValue, "name")
			if name == "envoy.filters.http.ext_authz" {
				typedConfig, _, _ := unstructured.NestedMap(patchValue, "typed_config")
				httpService, _, _ := unstructured.NestedMap(typedConfig, "http_service")
				serverUri, _, _ := unstructured.NestedMap(httpService, "server_uri")
				timeout, _, _ := unstructured.NestedString(serverUri, "timeout")
				return timeout
			}
		}
		return ""
	}, TestTimeout, TestInterval).Should(Equal("45s"), "EnvoyFilter should have custom timeout of 45s")

	patches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())

	var extAuthzTypedConfig map[string]interface{}
	for _, p := range patches {
		patch, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		patchValue, _, _ := unstructured.NestedMap(patch, "patch", "value")
		if patchValue == nil {
			continue
		}
		name, _, _ := unstructured.NestedString(patchValue, "name")
		if name == "envoy.filters.http.ext_authz" {
			extAuthzTypedConfig, _, _ = unstructured.NestedMap(patchValue, "typed_config")
			break
		}
	}
	g.Expect(extAuthzTypedConfig).NotTo(BeNil(), "ext_authz filter not found in EnvoyFilter")

	apiVersion, found, _ := unstructured.NestedString(extAuthzTypedConfig, "transport_api_version")
	g.Expect(found).To(BeTrue())
	g.Expect(apiVersion).To(Equal("V3"))

	httpService, found, _ := unstructured.NestedMap(extAuthzTypedConfig, "http_service")
	g.Expect(found).To(BeTrue(), "http_service not found in ext_authz config")

	serverUri, found, _ := unstructured.NestedMap(httpService, "server_uri")
	g.Expect(found).To(BeTrue(), "server_uri not found")

	uri, _, _ := unstructured.NestedString(serverUri, "uri")
	g.Expect(uri).To(ContainSubstring(gateway.KubeAuthProxyName))
	g.Expect(uri).To(ContainSubstring(gateway.GatewayNamespace))
	g.Expect(uri).To(ContainSubstring("/oauth2/auth"))

	cluster, _, _ := unstructured.NestedString(serverUri, "cluster")
	g.Expect(cluster).To(ContainSubstring("outbound|"))
	g.Expect(cluster).To(ContainSubstring(gateway.KubeAuthProxyName))

	timeout, _, _ := unstructured.NestedString(serverUri, "timeout")
	g.Expect(timeout).To(Equal("45s"))

	authRequest, found, _ := unstructured.NestedMap(httpService, "authorization_request")
	g.Expect(found).To(BeTrue())
	allowedHeaders, _, _ := unstructured.NestedMap(authRequest, "allowed_headers")
	g.Expect(allowedHeaders).NotTo(BeNil())

	authResponse, found, _ := unstructured.NestedMap(httpService, "authorization_response")
	g.Expect(found).To(BeTrue())
	upstreamHeaders, _, _ := unstructured.NestedMap(authResponse, "allowed_upstream_headers")
	g.Expect(upstreamHeaders).NotTo(BeNil())
	clientHeaders, _, _ := unstructured.NestedMap(authResponse, "allowed_client_headers")
	g.Expect(clientHeaders).NotTo(BeNil())
}

// RunEnvoyFilterOrderTest validates that ext_authz comes before lua token forwarding in the filter chain.
func RunEnvoyFilterOrderTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	ef := &unstructured.Unstructured{}
	ef.SetGroupVersionKind(gvk.EnvoyFilter)
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.AuthnFilterName,
			Namespace: gateway.GatewayNamespace,
		}, ef)
	}, TestTimeout, TestInterval).Should(Succeed())

	patches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())

	var filterNames []string
	for _, p := range patches {
		patch, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		patchValue, _, _ := unstructured.NestedMap(patch, "patch", "value")
		if patchValue == nil {
			continue
		}
		name, found, _ := unstructured.NestedString(patchValue, "name")
		if found && name != "" {
			filterNames = append(filterNames, name)
		}
	}

	extAuthzIndex, luaIndex := -1, -1
	for i, name := range filterNames {
		if name == "envoy.filters.http.ext_authz" {
			extAuthzIndex = i
		}
		if name == "envoy.lua" {
			luaIndex = i
		}
	}
	g.Expect(extAuthzIndex).NotTo(Equal(-1), "ext_authz filter not found")
	g.Expect(luaIndex).NotTo(Equal(-1), "lua token forwarding filter not found")
	g.Expect(extAuthzIndex).To(BeNumerically("<", luaIndex),
		"ext_authz filter must come before lua token forwarding filter for authentication to work correctly")
}

// RunEnvoyFilterLuaTokenForwardingTest validates that the envoy.lua filter contains token forwarding and cookie stripping.
func RunEnvoyFilterLuaTokenForwardingTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	ef := &unstructured.Unstructured{}
	ef.SetGroupVersionKind(gvk.EnvoyFilter)
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.AuthnFilterName,
			Namespace: gateway.GatewayNamespace,
		}, ef)
	}, TestTimeout, TestInterval).Should(Succeed())

	patches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())

	var luaInlineCode string
	for _, p := range patches {
		patch, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		patchValue, _, _ := unstructured.NestedMap(patch, "patch", "value")
		if patchValue == nil {
			continue
		}
		name, _, _ := unstructured.NestedString(patchValue, "name")
		if name == "envoy.lua" {
			typedConfig, _, _ := unstructured.NestedMap(patchValue, "typed_config")
			luaInlineCode, _, _ = unstructured.NestedString(typedConfig, "inline_code")
			break
		}
	}
	g.Expect(luaInlineCode).NotTo(BeEmpty(), "lua token forwarding filter not found")
	g.Expect(luaInlineCode).To(ContainSubstring("x-auth-request-access-token"))
	g.Expect(luaInlineCode).To(ContainSubstring("x-forwarded-access-token"))
	g.Expect(luaInlineCode).To(ContainSubstring("authorization"))
	g.Expect(luaInlineCode).To(ContainSubstring("Bearer"))
	g.Expect(luaInlineCode).To(ContainSubstring(gateway.AuthProxyCookieName))
	g.Expect(luaInlineCode).To(ContainSubstring("cookie"))
}

// RunEnvoyFilterLegacyRedirectPresentTest validates that the envoy.filters.http.lua.redirect filter is present when subdomain is not legacy.
func RunEnvoyFilterLegacyRedirectPresentTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	ef := &unstructured.Unstructured{}
	ef.SetGroupVersionKind(gvk.EnvoyFilter)
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.AuthnFilterName,
			Namespace: gateway.GatewayNamespace,
		}, ef)
	}, TestTimeout, TestInterval).Should(Succeed())

	patches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())

	var luaRedirectCode string
	for _, p := range patches {
		patch, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		patchValue, _, _ := unstructured.NestedMap(patch, "patch", "value")
		if patchValue == nil {
			continue
		}
		name, _, _ := unstructured.NestedString(patchValue, "name")
		if name == "envoy.filters.http.lua.redirect" {
			typedConfig, _, _ := unstructured.NestedMap(patchValue, "typed_config")
			luaRedirectCode, _, _ = unstructured.NestedString(typedConfig, "inline_code")
			break
		}
	}
	g.Expect(luaRedirectCode).NotTo(BeEmpty(), "Lua redirect filter should be present when subdomain is not legacy")
	g.Expect(luaRedirectCode).To(ContainSubstring("301"))
	g.Expect(luaRedirectCode).To(ContainSubstring("location"))
	g.Expect(luaRedirectCode).To(ContainSubstring("data%-science%-gateway"))
	g.Expect(luaRedirectCode).To(ContainSubstring(gateway.DefaultGatewaySubdomain))
}

// RunEnvoyFilterLegacyRedirectOrderFirstTest validates that the legacy redirect filter is first in the filter chain.
func RunEnvoyFilterLegacyRedirectOrderFirstTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	ef := &unstructured.Unstructured{}
	ef.SetGroupVersionKind(gvk.EnvoyFilter)
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.AuthnFilterName,
			Namespace: gateway.GatewayNamespace,
		}, ef)
	}, TestTimeout, TestInterval).Should(Succeed())

	patches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())

	var filterNames []string
	for _, p := range patches {
		patch, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		patchValue, _, _ := unstructured.NestedMap(patch, "patch", "value")
		if patchValue == nil {
			continue
		}
		name, found, _ := unstructured.NestedString(patchValue, "name")
		if found && name != "" {
			filterNames = append(filterNames, name)
		}
	}

	luaRedirectIndex, extAuthzIndex := -1, -1
	for i, name := range filterNames {
		if name == "envoy.filters.http.lua.redirect" {
			luaRedirectIndex = i
		}
		if name == "envoy.filters.http.ext_authz" {
			extAuthzIndex = i
		}
	}
	g.Expect(luaRedirectIndex).NotTo(Equal(-1), "lua redirect filter not found")
	g.Expect(extAuthzIndex).NotTo(Equal(-1), "ext_authz filter not found")
	g.Expect(luaRedirectIndex).To(BeNumerically("<", extAuthzIndex),
		"Legacy redirect filter must be FIRST (before ext_authz) to redirect legacy hostnames before auth processing")
}

// RunSpecMutationCookieConfigTest updates the Cookie spec and validates that Deployment args are updated accordingly.
func RunSpecMutationCookieConfigTest(t *testing.T, setup TestSetup, cookieUpdate serviceApi.CookieConfig) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() bool {
		deployment := &appsv1.Deployment{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment); err != nil {
			return false
		}
		for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
			if arg == defaultCookieExpireArg {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())

	mergedSpec := setup.Spec
	mergedSpec.Cookie = cookieUpdate
	UpdateGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient, mergedSpec)

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		return gc.Spec.Cookie.Expire == cookieUpdate.Expire && gc.Spec.Cookie.Refresh == cookieUpdate.Refresh
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig spec (Cookie) was not updated")

	g.Eventually(func() bool {
		deployment := &appsv1.Deployment{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment); err != nil {
			return false
		}
		args := deployment.Spec.Template.Spec.Containers[0].Args
		hasExpire := false
		hasRefresh := false
		for _, arg := range args {
			if arg == updatedCookieExpireArg {
				hasExpire = true
			}
			if arg == updatedCookieRefreshArg {
				hasRefresh = true
			}
		}
		return hasExpire && hasRefresh
	}, TestTimeout, TestInterval).Should(BeTrue(), "Deployment cookie args not updated after GatewayConfig spec change")
}

// RunDeploymentWithAllArgsTest validates Deployment replicas, security context, ports, env, volumes, and that provider args contain or omit the given slices.
func RunDeploymentWithAllArgsTest(t *testing.T, setup TestSetup, expectedHostname string, providerMustContainArgs, providerMustNotContainArgs []string) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		deployment := &appsv1.Deployment{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment)
	}, TestTimeout, TestInterval).Should(Succeed())

	deployment := &appsv1.Deployment{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxyName,
		Namespace: gateway.GatewayNamespace,
	}, deployment)).To(Succeed())
	assertOwnedByGatewayConfig(g, deployment)

	g.Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
	g.Expect(deployment.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", gateway.KubeAuthProxyName))

	g.Expect(deployment.Spec.Template.Spec.SecurityContext.RunAsNonRoot).NotTo(BeNil())
	g.Expect(*deployment.Spec.Template.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())
	g.Expect(deployment.Spec.Template.Spec.SecurityContext.SeccompProfile).NotTo(BeNil())
	g.Expect(deployment.Spec.Template.Spec.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))

	g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty())
	container := deployment.Spec.Template.Spec.Containers[0]
	g.Expect(container.Name).To(Equal(gateway.KubeAuthProxyName))

	g.Expect(container.SecurityContext.ReadOnlyRootFilesystem).NotTo(BeNil())
	g.Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
	g.Expect(container.SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
	g.Expect(*container.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
	g.Expect(container.SecurityContext.Capabilities).NotTo(BeNil())
	g.Expect(container.SecurityContext.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))

	g.Expect(container.Ports).To(HaveLen(3))
	hasHTTP, hasHTTPS, hasMetrics := false, false, false
	for _, port := range container.Ports {
		switch port.Name {
		case "http":
			hasHTTP = true
			g.Expect(port.ContainerPort).To(Equal(int32(gateway.AuthProxyHTTPPort)))
		case "https":
			hasHTTPS = true
			g.Expect(port.ContainerPort).To(Equal(int32(gateway.GatewayHTTPSPort)))
		case "metrics":
			hasMetrics = true
			g.Expect(port.ContainerPort).To(Equal(int32(gateway.AuthProxyMetricsPort)))
		}
	}
	g.Expect(hasHTTP).To(BeTrue())
	g.Expect(hasHTTPS).To(BeTrue())
	g.Expect(hasMetrics).To(BeTrue())

	g.Expect(container.Env).To(HaveLen(4))
	envNames := make(map[string]bool)
	for _, env := range container.Env {
		envNames[env.Name] = true
		if env.Name == "PROXY_MODE" {
			g.Expect(env.Value).To(Equal("auth"))
		} else {
			g.Expect(env.ValueFrom).NotTo(BeNil())
			g.Expect(env.ValueFrom.SecretKeyRef).NotTo(BeNil())
			g.Expect(env.ValueFrom.SecretKeyRef.Name).To(Equal(gateway.KubeAuthProxySecretsName))
		}
	}
	g.Expect(envNames).To(HaveKey(gateway.EnvClientID))
	g.Expect(envNames).To(HaveKey(gateway.EnvClientSecret))
	g.Expect(envNames).To(HaveKey(gateway.EnvCookieSecret))
	g.Expect(envNames).To(HaveKey("PROXY_MODE"))

	hasTLSMount, hasTmpMount := false, false
	for _, vm := range container.VolumeMounts {
		if vm.Name == gateway.TLSCertsVolumeName {
			hasTLSMount = true
			g.Expect(vm.MountPath).To(Equal(gateway.TLSCertsMountPath))
			g.Expect(vm.ReadOnly).To(BeTrue())
		}
		if vm.Name == "tmp" {
			hasTmpMount = true
			g.Expect(vm.MountPath).To(Equal("/tmp"))
		}
	}
	g.Expect(hasTLSMount).To(BeTrue(), "Should have TLS cert volume mount")
	g.Expect(hasTmpMount).To(BeTrue(), "Should have tmp volume mount")

	hasTLSVolume, hasTmpVolume := false, false
	for _, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == gateway.TLSCertsVolumeName {
			hasTLSVolume = true
			g.Expect(vol.Secret).NotTo(BeNil())
			g.Expect(vol.Secret.SecretName).To(Equal(gateway.KubeAuthProxyTLSName))
		}
		if vol.Name == "tmp" {
			hasTmpVolume = true
			g.Expect(vol.EmptyDir).NotTo(BeNil())
			g.Expect(vol.EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory))
		}
	}
	g.Expect(hasTLSVolume).To(BeTrue(), "Should have TLS cert volume")
	g.Expect(hasTmpVolume).To(BeTrue(), "Should have tmp volume")

	args := container.Args
	g.Expect(args).To(ContainElement(fmt.Sprintf("--http-address=0.0.0.0:%d", gateway.AuthProxyHTTPPort)))
	g.Expect(args).To(ContainElement(fmt.Sprintf("--https-address=0.0.0.0:%d", gateway.GatewayHTTPSPort)))
	g.Expect(args).To(ContainElement(fmt.Sprintf("--metrics-address=0.0.0.0:%d", gateway.AuthProxyMetricsPort)))
	g.Expect(args).To(ContainElement("--email-domain=*"))
	g.Expect(args).To(ContainElement("--upstream=static://200"))
	g.Expect(args).To(ContainElement("--skip-provider-button"))
	g.Expect(args).To(ContainElement("--skip-jwt-bearer-tokens=true"))
	g.Expect(args).To(ContainElement("--pass-access-token=true"))
	g.Expect(args).To(ContainElement("--set-xauthrequest=true"))
	g.Expect(args).To(ContainElement(fmt.Sprintf("--redirect-url=https://%s/oauth2/callback", expectedHostname)))
	g.Expect(args).To(ContainElement(fmt.Sprintf("--tls-cert-file=%s/tls.crt", gateway.TLSCertsMountPath)))
	g.Expect(args).To(ContainElement(fmt.Sprintf("--tls-key-file=%s/tls.key", gateway.TLSCertsMountPath)))
	g.Expect(args).To(ContainElement("--use-system-trust-store=true"))
	g.Expect(args).To(ContainElement(defaultCookieExpireArg))
	g.Expect(args).To(ContainElement(defaultCookieRefreshArg))
	g.Expect(args).To(ContainElement("--cookie-secure=true"))
	g.Expect(args).To(ContainElement("--cookie-httponly=true"))
	g.Expect(args).To(ContainElement("--cookie-samesite=lax"))
	g.Expect(args).To(ContainElement(fmt.Sprintf("--cookie-name=%s", gateway.AuthProxyCookieName)))
	g.Expect(args).To(ContainElement(fmt.Sprintf("--cookie-domain=%s", expectedHostname)))

	for _, arg := range providerMustContainArgs {
		g.Expect(args).To(ContainElement(arg))
	}
	for _, arg := range providerMustNotContainArgs {
		g.Expect(args).NotTo(ContainElement(arg))
	}

	g.Expect(deployment.Spec.Template.Annotations).To(HaveKey("opendatahub.io/secret-hash"))
}

// RunOCPRouteCreationTest validates that the main OCP Route is created with the expected hostname and TLS.
func RunOCPRouteCreationTest(t *testing.T, setup TestSetup, expectedHostname string) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		route := &routev1.Route{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}, route)
	}, TestTimeout, TestInterval).Should(Succeed())

	route := &routev1.Route{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.DefaultGatewayName,
		Namespace: gateway.GatewayNamespace,
	}, route)).To(Succeed())
	assertOwnedByGatewayConfig(g, route)

	g.Expect(route.Spec.Host).To(Equal(expectedHostname))
	g.Expect(route.Spec.To.Kind).To(Equal("Service"))
	g.Expect(route.Spec.To.Name).To(Equal(gateway.GatewayServiceFullName))
	g.Expect(route.Spec.Port.TargetPort.IntVal).To(Equal(int32(gateway.StandardHTTPSPort)))
	g.Expect(route.Spec.TLS.Termination).To(Equal(routev1.TLSTerminationReencrypt))
	g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(Equal(routev1.InsecureEdgeTerminationPolicyRedirect))
}

// RunLegacyRedirectRouteCreationTest verifies the legacy redirect OCP Route is created with expected host.
func RunLegacyRedirectRouteCreationTest(t *testing.T, setup TestSetup, expectedLegacyHost string) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	legacyRouteName := gateway.DefaultGatewayName + "-legacy-redirect"
	g.Eventually(func() error {
		route := &routev1.Route{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      legacyRouteName,
			Namespace: gateway.GatewayNamespace,
		}, route)
	}, TestTimeout, TestInterval).Should(Succeed())

	route := &routev1.Route{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      legacyRouteName,
		Namespace: gateway.GatewayNamespace,
	}, route)).To(Succeed())
	assertOwnedByGatewayConfig(g, route)
	g.Expect(route.Spec.Host).To(Equal(expectedLegacyHost))
}

// RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest creates GatewayConfig with legacy subdomain from the start
// and asserts the legacy redirect route is not created (template omits it when Subdomain == LegacyGatewaySubdomain).
// Does not rely on GC; DeleteGatewayConfig cleans up routes before the next test.
func RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)

	if setup.SetupFunc != nil {
		setup.SetupFunc(t, setup.TC)
	}
	spec := setup.Spec
	spec.Subdomain = gateway.LegacyGatewaySubdomain
	CreateGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient, spec)
	defer DeleteGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient)

	legacyRouteName := gateway.DefaultGatewayName + "-legacy-redirect"

	g.Eventually(func() bool {
		route := &routev1.Route{}
		err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      legacyRouteName,
			Namespace: gateway.GatewayNamespace,
		}, route)
		return k8serr.IsNotFound(err)
	}, TestTimeout, TestInterval).Should(BeTrue(),
		"Legacy redirect route should not exist when GatewayConfig subdomain is legacy from the start")
}

// RunNetworkPolicyDisabledTest validates that no new NetworkPolicy is created when spec has NetworkPolicy.Ingress.Enabled=false.
func RunNetworkPolicyDisabledTest(t *testing.T, setup TestSetup, spec serviceApi.GatewayConfigSpec) {
	g := NewWithT(t)

	var listBefore networkingv1.NetworkPolicyList
	g.Expect(setup.TC.K8sClient.List(setup.TC.Ctx, &listBefore, client.InNamespace(gateway.GatewayNamespace))).To(Succeed())
	countBefore := len(listBefore.Items)

	CreateGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient, spec)
	defer DeleteGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient)

	g.Eventually(func() error {
		deployment := &appsv1.Deployment{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment)
	}, TestTimeout, TestInterval).Should(Succeed())

	var listAfter networkingv1.NetworkPolicyList
	g.Expect(setup.TC.K8sClient.List(setup.TC.Ctx, &listAfter, client.InNamespace(gateway.GatewayNamespace))).To(Succeed())
	g.Expect(len(listAfter.Items)).To(BeNumerically("<=", countBefore),
		"NetworkPolicy count must not increase when Ingress.Enabled=false")
}

// RunLoadBalancerIngressModeTest validates that Gateway is shaped for LoadBalancer (no Infrastructure, hostname set) and no OCP Route is created.
// CreateGatewayConfig ensures no existing GatewayConfig before create. Pass spec with IngressModeLoadBalancer (e.g. oauthSpecWithLoadBalancer or oidcSpecWithLoadBalancer).
func RunLoadBalancerIngressModeTest(t *testing.T, tc *TestEnvContext, spec serviceApi.GatewayConfigSpec) {
	t.Helper()
	g := NewWithT(t)

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, spec)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Verify Gateway CR exists and is shaped for LoadBalancer mode (hostname set, no Infrastructure).
	g.Eventually(func() error {
		gw := &gwapiv1.Gateway{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}, gw); err != nil {
			return err
		}
		if string(gw.Spec.GatewayClassName) != gateway.GatewayClassName {
			return fmt.Errorf("GatewayClassName: got %q", gw.Spec.GatewayClassName)
		}
		if gw.Spec.Infrastructure != nil {
			return fmt.Errorf("LoadBalancer mode: Gateway should not have Infrastructure (ClusterIP config)")
		}
		var httpsListener *gwapiv1.Listener
		for i := range gw.Spec.Listeners {
			if gw.Spec.Listeners[i].Name == "https" {
				httpsListener = &gw.Spec.Listeners[i]
				break
			}
		}
		if httpsListener == nil {
			return fmt.Errorf("Gateway should have https listener")
		}
		if httpsListener.Hostname == nil {
			return fmt.Errorf("LoadBalancer mode: https listener should have hostname set")
		}
		return nil
	}, TestTimeout, TestInterval).Should(Succeed())

	// Verify no OCP Route exists in LoadBalancer mode.
	g.Eventually(func() bool {
		route := &routev1.Route{}
		err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}, route)
		return client.IgnoreNotFound(err) == nil && err != nil
	}, TestTimeout, TestInterval).Should(BeTrue(), "OCP Route should not exist in LoadBalancer mode (GC removes stale Routes)")
}

// RunNginxDashboardRedirectCreationTest validates that nginx-based dashboard redirect resources exist in the application namespace:
// ConfigMap (redirect.conf with 301 to gateway host), Deployment, Service, and dashboard Route (odh-dashboard or rhods-dashboard).
func RunNginxDashboardRedirectCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	appNs := cluster.GetApplicationNamespace()
	nnCM := types.NamespacedName{Name: gateway.DashboardRedirectConfigName, Namespace: appNs}
	nnApp := types.NamespacedName{Name: gateway.DashboardRedirectName, Namespace: appNs}

	// ConfigMap
	var cm corev1.ConfigMap
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, nnCM, &cm)
	}, TestTimeout, TestInterval).Should(Succeed())
	assertOwnedByGatewayConfig(g, &cm)
	g.Expect(cm.Labels).To(HaveKeyWithValue("app", gateway.DashboardRedirectName))
	g.Expect(cm.Data).To(HaveKey("redirect.conf"))
	redirectConf := cm.Data["redirect.conf"]
	g.Expect(redirectConf).To(ContainSubstring("location /"))
	g.Expect(redirectConf).To(ContainSubstring("return 301"))
	g.Expect(redirectConf).To(ContainSubstring("$request_uri"))
	gatewayHost, err := gateway.GetGatewayDomain(setup.TC.Ctx, setup.TC.K8sClient)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gatewayHost).NotTo(BeEmpty())
	g.Expect(redirectConf).To(ContainSubstring("https://" + gatewayHost))

	// Deployment
	var dep appsv1.Deployment
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, nnApp, &dep)
	}, TestTimeout, TestInterval).Should(Succeed())
	assertOwnedByGatewayConfig(g, &dep)
	g.Expect(dep.Spec.Replicas).NotTo(BeNil())
	g.Expect(*dep.Spec.Replicas).To(BeNumerically(">=", 1))
	g.Expect(dep.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", gateway.DashboardRedirectName))
	g.Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
	g.Expect(dep.Spec.Template.Spec.Containers[0].Image).NotTo(BeEmpty())

	// Service
	var svc corev1.Service
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, nnApp, &svc)
	}, TestTimeout, TestInterval).Should(Succeed())
	assertOwnedByGatewayConfig(g, &svc)
	g.Expect(svc.Spec.Selector).To(HaveKeyWithValue("app", gateway.DashboardRedirectName))

	dashboardRouteName := gateway.DashboardRouteNameODH
	if cluster.GetRelease().Name == cluster.SelfManagedRhoai || cluster.GetRelease().Name == cluster.ManagedRhoai {
		dashboardRouteName = gateway.DashboardRouteNameRHOAI
	}

	// Route
	var route routev1.Route
	g.Eventually(func() error {
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: dashboardRouteName, Namespace: appNs}, &route)
	}, TestTimeout, TestInterval).Should(Succeed())
	assertOwnedByGatewayConfig(g, &route)
	g.Expect(route.Spec.To.Name).To(Equal(gateway.DashboardRedirectName))
}

// RunNetworkPolicyCreationTest validates that the NetworkPolicy is created with pod selector and ingress rules.
func RunNetworkPolicyCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		np := &networkingv1.NetworkPolicy{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, np)
	}, TestTimeout, TestInterval).Should(Succeed())

	np := &networkingv1.NetworkPolicy{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxyName,
		Namespace: gateway.GatewayNamespace,
	}, np)).To(Succeed())

	g.Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", gateway.KubeAuthProxyName))
	g.Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
	g.Expect(np.Spec.Ingress).To(HaveLen(3))
}

// RunGatewayConfigStatusConditionsTest validates that GatewayConfig status gets conditions set (e.g. Ready).
func RunGatewayConfigStatusConditionsTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		deployment := &appsv1.Deployment{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment)
	}, TestTimeout, TestInterval).Should(Succeed(), "Deployment must exist before checking status conditions")

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		if gc.Spec.IngressMode != setup.Spec.IngressMode {
			return false
		}
		if setup.Spec.Subdomain == "" {
			return gc.Spec.Subdomain == "" || gc.Spec.Subdomain == gateway.DefaultGatewaySubdomain
		}
		return gc.Spec.Subdomain == setup.Spec.Subdomain
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig spec should match setup before checking status conditions")

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		return len(gc.Status.Conditions) > 0
	}, TestTimeout, TestInterval).Should(BeTrue())
}

// RunDestinationRuleCreationTest validates that the DestinationRule is created with the expected traffic policy.
func RunDestinationRuleCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		dr := &unstructured.Unstructured{}
		dr.SetGroupVersionKind(gvk.DestinationRule)
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.DestinationRuleName,
			Namespace: gateway.GatewayNamespace,
		}, dr)
	}, TestTimeout, TestInterval).Should(Succeed())

	dr := &unstructured.Unstructured{}
	dr.SetGroupVersionKind(gvk.DestinationRule)
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
		Name:      gateway.DestinationRuleName,
		Namespace: gateway.GatewayNamespace,
	}, dr)).To(Succeed())
	assertOwnedByGatewayConfig(g, dr)

	// Verify host is wildcard
	host, found, err := unstructured.NestedString(dr.Object, "spec", "host")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(host).To(Equal("*"))
}

// RunGatewayConfigStatusDomainTest validates that GatewayConfig status domain is set to the expected FQDN.
func RunGatewayConfigStatusDomainTest(t *testing.T, setup TestSetup, expectedDomain string) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		deployment := &appsv1.Deployment{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment)
	}, TestTimeout, TestInterval).Should(Succeed(), "Deployment must exist before checking status domain")

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		if gc.Spec.IngressMode != setup.Spec.IngressMode {
			return false
		}
		// When setup did not set Subdomain (empty), accept either empty or default—both yield the same status domain.
		if setup.Spec.Subdomain == "" {
			return gc.Spec.Subdomain == "" || gc.Spec.Subdomain == gateway.DefaultGatewaySubdomain
		}
		return gc.Spec.Subdomain == setup.Spec.Subdomain
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig spec should match setup before checking status domain")

	g.Eventually(func() error {
		gc := &serviceApi.GatewayConfig{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return err
		}
		if gc.Status.Domain == "" {
			return fmt.Errorf("GatewayConfig status domain not set yet")
		}
		return nil
	}, TestTimeout, TestInterval).Should(Succeed())

	gc := &serviceApi.GatewayConfig{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc)).To(Succeed())
	g.Expect(gc.Status.Domain).To(Equal(expectedDomain))
}
