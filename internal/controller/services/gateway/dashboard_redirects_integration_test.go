//go:build integration

//nolint:testpackage
package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

type dashboardRedirectTestEnv struct {
	env    *envtest.Environment
	client client.Client
}

func TestCreateDashboardRedirectsWithRealAPIServer(t *testing.T) {
	tc := startDashboardRedirectTestEnv(t, t.Context())

	t.Run("when dashboard crd is not registered", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()
		tc.resetDashboardRedirectTestState(t, ctx, false)

		dashboard := resources.GvkToUnstructured(gvk.Dashboard)
		err := tc.client.Get(ctx, client.ObjectKey{Name: componentApi.DashboardInstanceName}, dashboard)
		g.Expect(err).To(Satisfy(meta.IsNoMatchError), "Dashboard CRD should not be registered in this case")

		gatewayConfig := tc.createDashboardRedirectGatewayConfig(t, ctx)
		redirectObjects := tc.createDashboardRedirectResources(t, ctx)

		templates, err := createDashboardRedirects(ctx, tc.client, gatewayConfig)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(templates).To(BeEmpty())

		for _, obj := range redirectObjects {
			g.Expect(tc.client.Get(ctx, client.ObjectKeyFromObject(obj), obj)).
				To(MatchError(ContainSubstring("not found")))
		}
	})

	t.Run("when dashboard crd is registered but cr is absent", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()
		tc.installDashboardCRD(t, ctx)
		tc.resetDashboardRedirectTestState(t, ctx, true)

		dashboard := resources.GvkToUnstructured(gvk.Dashboard)
		err := tc.client.Get(ctx, client.ObjectKey{Name: componentApi.DashboardInstanceName}, dashboard)
		g.Expect(err).To(Satisfy(k8serr.IsNotFound), "Dashboard CR should be absent in this case")

		gatewayConfig := tc.createDashboardRedirectGatewayConfig(t, ctx)
		redirectObjects := tc.createDashboardRedirectResources(t, ctx)

		templates, err := createDashboardRedirects(ctx, tc.client, gatewayConfig)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(templates).To(BeEmpty())

		for _, obj := range redirectObjects {
			g.Expect(tc.client.Get(ctx, client.ObjectKeyFromObject(obj), obj)).
				To(MatchError(ContainSubstring("not found")))
		}
	})

	t.Run("when dashboard cr is present", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()
		tc.installDashboardCRD(t, ctx)
		tc.resetDashboardRedirectTestState(t, ctx, true)

		gatewayConfig := tc.createDashboardRedirectGatewayConfig(t, ctx)
		dashboard := resources.GvkToUnstructured(gvk.Dashboard)
		dashboard.SetName(componentApi.DashboardInstanceName)
		g.Expect(tc.client.Create(ctx, dashboard)).To(Succeed())
		g.Expect(tc.client.Get(ctx, client.ObjectKey{Name: componentApi.DashboardInstanceName}, dashboard)).To(Succeed())

		templates, err := createDashboardRedirects(ctx, tc.client, gatewayConfig)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(templates).To(HaveLen(5))
	})
}

func startDashboardRedirectTestEnv(t *testing.T, ctx context.Context) *dashboardRedirectTestEnv {
	t.Helper()
	g := NewWithT(t)

	rootPath, err := envtestutil.FindProjectRoot()
	g.Expect(err).ToNot(HaveOccurred())

	scheme, err := testscheme.New()
	g.Expect(err).ToNot(HaveOccurred())

	crdPaths := []string{
		filepath.Join(rootPath, "config", "crd", "bases", "services.platform.opendatahub.io_gatewayconfigs.yaml"),
		filepath.Join(rootPath, "config", "crd", "external", "route.openshift.io_routes.yaml"),
	}

	testEnv := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme:             scheme,
			Paths:              crdPaths,
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())

	cli, err := client.New(cfg, client.Options{Scheme: scheme})
	g.Expect(err).ToNot(HaveOccurred())

	appNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.GetApplicationNamespace()}}
	g.Expect(cli.Create(ctx, appNs)).To(Succeed())

	t.Cleanup(func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	})

	return &dashboardRedirectTestEnv{
		env:    testEnv,
		client: cli,
	}
}

func (tc *dashboardRedirectTestEnv) createDashboardRedirectGatewayConfig(
	t *testing.T,
	ctx context.Context,
) *serviceApi.GatewayConfig {
	t.Helper()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}
	g.Expect(tc.client.Create(ctx, gatewayConfig)).To(Succeed())

	return gatewayConfig
}

func (tc *dashboardRedirectTestEnv) createDashboardRedirectResources(
	t *testing.T,
	ctx context.Context,
) []client.Object {
	t.Helper()
	g := NewWithT(t)

	appNs := cluster.GetApplicationNamespace()
	objs := []client.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      DashboardRedirectConfigName,
				Namespace: appNs,
			},
		},
	}

	for _, obj := range objs {
		g.Expect(tc.client.Create(ctx, obj)).To(Succeed())
	}

	return objs
}

func (tc *dashboardRedirectTestEnv) installDashboardCRD(t *testing.T, ctx context.Context) {
	t.Helper()
	g := NewWithT(t)

	crd := tc.getDashboardCRDForIntegration()
	g.Expect(client.IgnoreAlreadyExists(tc.client.Create(ctx, crd))).To(Succeed())

	g.Eventually(func() bool {
		dashboard := resources.GvkToUnstructured(gvk.Dashboard)
		dashboard.SetName(componentApi.DashboardInstanceName)

		err := tc.client.Get(ctx, client.ObjectKey{Name: componentApi.DashboardInstanceName}, dashboard)
		return err == nil || k8serr.IsNotFound(err)
	}, 5*time.Second, 100*time.Millisecond).Should(
		BeTrue(),
		"Dashboard CRD should become queryable before the CR-present/absent cases run",
	)
}

func (tc *dashboardRedirectTestEnv) resetDashboardRedirectTestState(
	t *testing.T,
	ctx context.Context,
	includeDashboardCRD bool,
) {
	t.Helper()

	appNs := cluster.GetApplicationNamespace()
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DashboardRedirectConfigName,
			Namespace: appNs,
		},
	}
	tc.deleteObjectEventually(t, ctx, configMap)

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}
	tc.deleteObjectEventually(t, ctx, gatewayConfig)

	if includeDashboardCRD {
		dashboard := resources.GvkToUnstructured(gvk.Dashboard)
		dashboard.SetName(componentApi.DashboardInstanceName)
		tc.deleteObjectEventually(t, ctx, dashboard)
	}
}

func (tc *dashboardRedirectTestEnv) deleteObjectEventually(
	t *testing.T,
	ctx context.Context,
	obj client.Object,
) {
	t.Helper()

	g := NewWithT(t)
	key := client.ObjectKeyFromObject(obj)

	g.Eventually(func() error {
		return client.IgnoreNotFound(tc.client.Delete(ctx, obj))
	}).Should(Succeed())

	g.Eventually(func() error {
		return tc.client.Get(ctx, key, obj)
	}).Should(Satisfy(k8serr.IsNotFound))
}

func (tc *dashboardRedirectTestEnv) getDashboardCRDForIntegration() *apiextensionsv1.CustomResourceDefinition {
	preserveUnknown := true

	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dashboards." + gvk.Dashboard.Group,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Dashboard.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     gvk.Dashboard.Kind,
				ListKind: gvk.Dashboard.Kind + "List",
				Plural:   "dashboards",
				Singular: "dashboard",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    gvk.Dashboard.Version,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: &preserveUnknown,
					},
				},
			}},
		},
	}
}
