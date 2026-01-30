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
	oauthv1 "github.com/openshift/api/oauth/v1"
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
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

// Common test constants.
const (
	TestTimeout  = 60 * time.Second
	TestInterval = 500 * time.Millisecond

	// controllerUserName is the name of the user created for the controller in envtest.
	controllerUserName = "gateway-controller"
)

// Shared test environments for OAuth and OIDC modes.
// These are initialized once per test run to avoid slow envtest startup for each test.
var (
	OAuthTestEnv *TestEnvContext
	OIDCTestEnv  *TestEnvContext
)

// TestEnvContext holds the test environment state.
type TestEnvContext struct {
	Ctx       context.Context
	Cancel    context.CancelFunc
	TestEnv   *envtest.Environment
	K8sClient client.Client
	Scheme    *runtime.Scheme
}

// readClusterRolesFromFile reads ClusterRole objects from a YAML file (supports multi-document YAML).
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

// setupRBACForUser creates ClusterRole from role.yaml and binds it to the specified user.
// This ensures the controller runs with the actual RBAC permissions defined in config/rbac/role.yaml.
func setupRBACForUser(ctx context.Context, cli client.Client, rootPath string, userName string) error {
	// Read ClusterRoles from config/rbac/role.yaml
	roleYamlPath := filepath.Join(rootPath, "config", "rbac", "role.yaml")
	roles, err := readClusterRolesFromFile(roleYamlPath)
	if err != nil {
		return fmt.Errorf("failed to read role.yaml: %w", err)
	}

	if len(roles) == 0 {
		return fmt.Errorf("no ClusterRole found in %s", roleYamlPath)
	}

	// Create each ClusterRole and bind it to the user
	for _, role := range roles {
		// Create ClusterRole
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

// createControllerUser creates a user via envtest's AddUser and returns its REST config.
// This provides proper RBAC enforcement in tests.
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

// SetupTestEnvForMain creates and starts the envtest environment for use in TestMain.
// This avoids the overhead of creating a new environment for each test.
func SetupTestEnvForMain(authMode string, clusterDomain string) *TestEnvContext {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	rootPath, err := envtestutil.FindProjectRoot()
	if err != nil {
		panic(fmt.Sprintf("Failed to find project root: %v", err))
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(routev1.Install(scheme))
	utilruntime.Must(oauthv1.AddToScheme(scheme))
	utilruntime.Must(configv1.Install(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(infrav1.AddToScheme(scheme))
	utilruntime.Must(dsciv2.AddToScheme(scheme))

	testEnv := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: scheme,
			Paths: []string{
				filepath.Join(rootPath, "config", "crd", "bases"),
				filepath.Join(rootPath, "config", "crd", "external"),
			},
			// Add Istio CRDs as objects (no external YAML files needed)
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

	// Admin client for setup (uses full privileges)
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to create k8s client: %v", err))
	}

	// Create a user for the controller with limited RBAC permissions
	// This uses envtest's AddUser which provides proper authentication
	controllerCfg, err := createControllerUser(testEnv, cfg)
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to create controller user: %v", err))
	}

	// Setup RBAC: create ClusterRole from role.yaml and bind to the controller user
	// This ensures the controller runs with the actual permissions defined in config/rbac/role.yaml
	if err := setupRBACForUser(ctx, k8sClient, rootPath, controllerUserName); err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to setup RBAC: %v", err))
	}

	// Setup cluster prerequisites using admin client (we're in TestMain)
	setupClusterPrerequisitesForMain(ctx, k8sClient, authMode, clusterDomain)

	// Setup manager with gateway controller using the RBAC-limited user config
	// This makes the controller run with actual RBAC permissions from role.yaml
	skipNameValidation := true
	mgr, err := ctrl.NewManager(controllerCfg, ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,
		Metrics: ctrlmetrics.Options{
			BindAddress: "0",
		},
		Controller: ctrlconfig.Controller{
			SkipNameValidation: &skipNameValidation,
		},
	})
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to create manager: %v", err))
	}

	handler := &gateway.ServiceHandler{}
	if err := handler.NewReconciler(ctx, mgr); err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to setup controller: %v", err))
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			fmt.Printf("Manager stopped with error: %v\n", err)
		}
	}()

	return &TestEnvContext{
		Ctx:       ctx,
		Cancel:    cancel,
		TestEnv:   testEnv,
		K8sClient: k8sClient,
		Scheme:    scheme,
	}
}

// setupClusterPrerequisitesForMain creates required cluster resources (for TestMain).
func setupClusterPrerequisitesForMain(ctx context.Context, cli client.Client, authMode, clusterDomain string) {
	// Create gateway namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: gateway.GatewayNamespace,
		},
	}
	if err := cli.Create(ctx, ns); err != nil {
		panic(fmt.Sprintf("Failed to create namespace: %v", err))
	}

	// Create cluster Ingress
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

	// Create cluster Authentication
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

	// Get applications namespace based on platform type (ODH vs RHOAI)
	appsNamespace := cluster.GetApplicationNamespace()

	// Create applications namespace (required by DSCI)
	appsNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: appsNamespace,
		},
	}
	if err := cli.Create(ctx, appsNs); err != nil {
		panic(fmt.Sprintf("Failed to create applications namespace: %v", err))
	}

	// Create DSCInitialization (required by gateway controller)
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: appsNamespace,
		},
	}
	if err := cli.Create(ctx, dsci); err != nil {
		panic(fmt.Sprintf("Failed to create DSCInitialization: %v", err))
	}
}

// getIstioCRDs returns Istio CRDs (EnvoyFilter, DestinationRule) for envtest.
// This eliminates the need for external YAML files for these CRDs.
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

	// DestinationRule CRD - using v1 as that's what the controller expects (see pkg/cluster/gvk/gvk.go)
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

// CreateGatewayConfig creates a GatewayConfig resource.
// It waits for any existing GatewayConfig to be deleted first (for test isolation).
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
}

// DeleteGatewayConfig deletes the GatewayConfig.
func DeleteGatewayConfig(t *testing.T, ctx context.Context, cli client.Client) {
	t.Helper()
	gc := &serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err == nil {
		if err := cli.Delete(ctx, gc); err != nil {
			t.Logf("Warning: failed to delete GatewayConfig: %v", err)
		}
	}
}

// UpdateGatewayConfig updates the GatewayConfig spec.
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

// ============================================================================
// Common Test Infrastructure for OAuth/OIDC
// ============================================================================

// TestSetup holds the configuration for running a test with either OAuth or OIDC mode.
type TestSetup struct {
	TC        *TestEnvContext
	Spec      serviceApi.GatewayConfigSpec
	SetupFunc func(t *testing.T, tc *TestEnvContext) // optional pre-test setup (e.g., create OIDC secret)
}

// Setup runs the optional setup function and creates the GatewayConfig.
// Returns a cleanup function that should be deferred.
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

// ============================================================================
// Common Test Functions (used by both OAuth and OIDC tests)
// ============================================================================

// RunGatewayClassCreationTest verifies GatewayClass is created correctly.
func RunGatewayClassCreationTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() error {
		gc := &gwapiv1.GatewayClass{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: gateway.GatewayClassName}, gc)
	}, TestTimeout, TestInterval).Should(Succeed())

	gc := &gwapiv1.GatewayClass{}
	g.Expect(setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: gateway.GatewayClassName}, gc)).To(Succeed())
	g.Expect(string(gc.Spec.ControllerName)).To(Equal(gateway.GatewayControllerName))
}

// RunGatewayCreationTest verifies Gateway is created with correct class and HTTPS listener.
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

// RunHTTPRouteCreationTest verifies HTTPRoute is created with correct parentRef, path match, and backend.
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

// RunServiceCreationTest verifies Service is created correctly with all ports and annotations.
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

// RunAuthProxySecretCreationTest verifies kube-auth-proxy-creds secret exists with required keys and non-empty values.
// If expectedClientID is non-empty, also asserts secret.Data[EnvClientID] equals expectedClientID (e.g. for OIDC).
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

// RunHPACreationTest verifies HPA is created with correct scaling configuration.
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

// RunEnvoyFilterCreationTest verifies EnvoyFilter is created with workload selector and configPatches.
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

	selector, found, err := unstructured.NestedStringMap(ef.Object, "spec", "workloadSelector", "labels")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(selector).To(HaveKeyWithValue("gateway.networking.k8s.io/gateway-name", gateway.DefaultGatewayName))

	patches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(len(patches)).To(BeNumerically(">=", 2))
}

// RunEnvoyFilterExtAuthzConfigurationTest verifies EnvoyFilter ext_authz config (timeout, http_service, headers).
// Setup must use a spec with AuthProxyTimeout set (e.g. 45s) so the helper can assert the timeout.
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

// RunEnvoyFilterOrderTest verifies ext_authz comes before lua token forwarding filter.
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

// RunEnvoyFilterLuaTokenForwardingTest verifies envoy.lua filter contains token forwarding and cookie stripping.
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

// RunEnvoyFilterLegacyRedirectPresentTest verifies envoy.filters.http.lua.redirect filter is present when subdomain is not legacy.
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

// RunEnvoyFilterLegacyRedirectOrderFirstTest verifies legacy redirect filter is first in the filter chain.
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

// RunSpecMutationCookieConfigTest creates config, waits for default cookie args, updates Cookie spec, verifies deployment args.
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
			if arg == "--cookie-expire=24h0m0s" {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())

	mergedSpec := setup.Spec
	mergedSpec.Cookie = cookieUpdate
	UpdateGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient, mergedSpec)

	g.Eventually(func() bool {
		deployment := &appsv1.Deployment{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment); err != nil {
			return false
		}
		args := deployment.Spec.Template.Spec.Containers[0].Args
		hasNewExpire, hasNewRefresh := false, false
		for _, arg := range args {
			if arg == "--cookie-expire=48h0m0s" {
				hasNewExpire = true
			}
			if arg == "--cookie-refresh=2h0m0s" {
				hasNewRefresh = true
			}
		}
		return hasNewExpire && hasNewRefresh
	}, TestTimeout, TestInterval).Should(BeTrue())
}

// RunDeploymentWithAllArgsTest verifies Deployment replicas, security context, ports, env, volumes, and args.
// expectedHostname is the FQDN (e.g. subdomain + cluster domain). providerMustContainArgs are args that must be present (e.g. OAuth or OIDC specific).
// providerMustNotContainArgs are args that must NOT be present (e.g. OIDC test excludes OAuth-only args).
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
	g.Expect(args).To(ContainElement("--cookie-expire=24h0m0s"))
	g.Expect(args).To(ContainElement("--cookie-refresh=1h0m0s"))
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

// RunOCPRouteCreationTest verifies the main OCP Route is created with expected hostname and TLS.
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
	g.Expect(route.Spec.Host).To(Equal(expectedLegacyHost))
}

// RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest creates config, waits for legacy redirect route,
// updates subdomain to legacy, then asserts the legacy redirect route is deleted by GC.
// Caller may t.Skip() before invoking; when un-skipped this verifies GC behavior (envtest may not support GC).
func RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)

	if setup.SetupFunc != nil {
		setup.SetupFunc(t, setup.TC)
	}
	CreateGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient, setup.Spec)
	defer DeleteGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient)

	legacyRouteName := gateway.DefaultGatewayName + "-legacy-redirect"

	g.Eventually(func() error {
		route := &routev1.Route{}
		return setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      legacyRouteName,
			Namespace: gateway.GatewayNamespace,
		}, route)
	}, TestTimeout, TestInterval).Should(Succeed(), "Legacy redirect route should be created with default subdomain")

	mergedSpec := setup.Spec
	mergedSpec.Subdomain = gateway.LegacyGatewaySubdomain
	UpdateGatewayConfig(t, setup.TC.Ctx, setup.TC.K8sClient, mergedSpec)

	g.Eventually(func() bool {
		route := &routev1.Route{}
		err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{
			Name:      legacyRouteName,
			Namespace: gateway.GatewayNamespace,
		}, route)
		return k8serr.IsNotFound(err)
	}, 30*time.Second, TestInterval).Should(BeTrue(),
		"Legacy redirect route should be deleted by GC when subdomain changes to legacy")
}

// RunNetworkPolicyDisabledTest verifies that when spec has NetworkPolicy.Ingress.Enabled=false,
// the controller does not create a new NetworkPolicy. Uses count-before/count-after for shared env.
// Caller must ensure any auth-specific prerequisites (e.g. OIDC secret) exist before calling.
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

// RunNetworkPolicyCreationTest verifies NetworkPolicy is created with pod selector and ingress rules.
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

// RunGatewayConfigStatusConditionsTest verifies GatewayConfig status gets conditions set.
func RunGatewayConfigStatusConditionsTest(t *testing.T, setup TestSetup) {
	g := NewWithT(t)
	defer setup.Setup(t)()

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := setup.TC.K8sClient.Get(setup.TC.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		return len(gc.Status.Conditions) > 0
	}, TestTimeout, TestInterval).Should(BeTrue())
}

// RunDestinationRuleCreationTest verifies DestinationRule is created correctly.
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

	// Verify host is wildcard
	host, found, err := unstructured.NestedString(dr.Object, "spec", "host")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(host).To(Equal("*"))
}

// RunGatewayConfigStatusDomainTest verifies GatewayConfig status domain is set to the expected FQDN.
// expectedDomain is the full domain (e.g. gateway.DefaultGatewaySubdomain + "." + clusterDomain).
func RunGatewayConfigStatusDomainTest(t *testing.T, setup TestSetup, expectedDomain string) {
	g := NewWithT(t)
	defer setup.Setup(t)()

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
