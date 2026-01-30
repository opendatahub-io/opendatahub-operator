//go:build integration

/*
Copyright 2025.

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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		panic(fmt.Sprintf("Failed to create k8s client: %v", err))
	}

	// Setup cluster prerequisites using a nil testing.T (we're in TestMain)
	setupClusterPrerequisitesForMain(ctx, k8sClient, authMode, clusterDomain)

	// Setup manager with gateway controller
	skipNameValidation := true
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
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

// SetupTestEnv creates and starts the envtest environment.
func SetupTestEnv(t *testing.T, authMode string, clusterDomain string) *TestEnvContext {
	t.Helper()

	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	rootPath, err := envtestutil.FindProjectRoot()
	if err != nil {
		t.Fatalf("Failed to find project root: %v", err)
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
		t.Fatalf("Failed to start envtest: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		t.Fatalf("Failed to create k8s client: %v", err)
	}

	// Setup cluster prerequisites
	SetupClusterPrerequisites(t, ctx, k8sClient, authMode, clusterDomain)

	// Setup manager with gateway controller
	// SkipNameValidation is required because each test creates its own controller
	// and they would otherwise conflict on the controller name
	skipNameValidation := true
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
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
		t.Fatalf("Failed to create manager: %v", err)
	}

	handler := &gateway.ServiceHandler{}
	if err := handler.NewReconciler(ctx, mgr); err != nil {
		cancel()
		testEnv.Stop() //nolint:errcheck
		t.Fatalf("Failed to setup controller: %v", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Logf("Manager stopped with error: %v", err)
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

// Cleanup stops the test environment.
func (tc *TestEnvContext) Cleanup(t *testing.T) {
	t.Helper()
	tc.Cancel()
	if err := tc.TestEnv.Stop(); err != nil {
		t.Logf("Warning: failed to stop envtest: %v", err)
	}
}

// SetupClusterPrerequisites creates required cluster resources.
func SetupClusterPrerequisites(t *testing.T, ctx context.Context, cli client.Client, authMode, clusterDomain string) {
	t.Helper()

	// Create gateway namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: gateway.GatewayNamespace,
		},
	}
	if err := cli.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
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
		t.Fatalf("Failed to create Ingress: %v", err)
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
		t.Fatalf("Failed to create Authentication: %v", err)
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
		t.Fatalf("Failed to create applications namespace: %v", err)
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
		t.Fatalf("Failed to create DSCInitialization: %v", err)
	}
}

// CreateGatewayConfig creates a GatewayConfig resource.
// It waits for any existing GatewayConfig to be deleted first (for test isolation).
func CreateGatewayConfig(t *testing.T, ctx context.Context, cli client.Client, spec serviceApi.GatewayConfigSpec) {
	t.Helper()

	// Wait for any existing GatewayConfig to be fully deleted (test isolation)
	for i := 0; i < 60; i++ {
		gc := &serviceApi.GatewayConfig{}
		err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc)
		if client.IgnoreNotFound(err) == nil && err != nil {
			break // GatewayConfig doesn't exist, we can proceed
		}
		time.Sleep(500 * time.Millisecond)
	}

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

// ContainsString checks if a string contains a substring.
func ContainsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
