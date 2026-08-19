//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestCreateDashboardRedirects_SkipsWhenDashboardNotDeployed(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).To(Succeed())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: gatewayConfig,
	}

	err = createDashboardRedirects(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(rr.Templates).To(BeEmpty(), "no redirect templates should be added when Dashboard CR does not exist")
}

func TestCreateDashboardRedirects_CreatesWhenDashboardExists(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}

	dashboard := resources.GvkToUnstructured(gvk.Dashboard)
	dashboard.SetName(componentApi.DashboardInstanceName)

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, dashboard))
	g.Expect(err).To(Succeed())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: gatewayConfig,
	}

	err = createDashboardRedirects(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(rr.Templates).ToNot(BeEmpty(), "redirect templates should be added when Dashboard CR exists")
}

func TestCreateDashboardRedirects_SkipsWhenEnvDisabled(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	t.Setenv("DISABLE_DASHBOARD_REDIRECTS", "true")

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}

	dashboard := resources.GvkToUnstructured(gvk.Dashboard)
	dashboard.SetName(componentApi.DashboardInstanceName)

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, dashboard))
	g.Expect(err).To(Succeed())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: gatewayConfig,
	}

	err = createDashboardRedirects(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(rr.Templates).To(BeEmpty(), "no redirect templates should be added when DISABLE_DASHBOARD_REDIRECTS=true")
}

func TestCreateDashboardRedirects_SkipsWhenDashboardCRDNotRegistered(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}

	noMatchErr := &meta.NoKindMatchError{
		GroupKind:        schema.GroupKind{Group: componentApi.GroupVersion.Group, Kind: componentApi.DashboardKind},
		SearchedVersions: []string{componentApi.GroupVersion.Version},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(gatewayConfig),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if obj.GetObjectKind().GroupVersionKind() == gvk.Dashboard {
					return noMatchErr
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}),
	)
	g.Expect(err).To(Succeed())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: gatewayConfig,
	}

	err = createDashboardRedirects(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(rr.Templates).To(BeEmpty(), "no redirect templates should be added when Dashboard CRD is not registered")
}

func TestCreateDashboardRedirects_DeletesResourcesWhenDashboardRemoved(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	appNs := cluster.GetApplicationNamespace()

	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
	}

	redirectDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DashboardRedirectName,
			Namespace: appNs,
		},
	}

	redirectService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DashboardRedirectName,
			Namespace: appNs,
		},
	}

	redirectConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DashboardRedirectConfigName,
			Namespace: appNs,
		},
	}

	redirectRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetDashboardRouteName(),
			Namespace: appNs,
		},
	}

	legacyRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LegacyGatewaySubdomain,
			Namespace: appNs,
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(
		gatewayConfig,
		redirectDeployment,
		redirectService,
		redirectConfigMap,
		redirectRoute,
		legacyRoute,
	))
	g.Expect(err).To(Succeed())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: gatewayConfig,
	}

	err = createDashboardRedirects(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(rr.Templates).To(BeEmpty(), "no redirect templates should be added when Dashboard CR does not exist")

	// Verify all redirect resources were deleted
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(redirectDeployment), &appsv1.Deployment{})).
		To(MatchError(ContainSubstring("not found")))
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(redirectService), &corev1.Service{})).
		To(MatchError(ContainSubstring("not found")))
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(redirectConfigMap), &corev1.ConfigMap{})).
		To(MatchError(ContainSubstring("not found")))
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(redirectRoute), &routev1.Route{})).
		To(MatchError(ContainSubstring("not found")))
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(legacyRoute), &routev1.Route{})).
		To(MatchError(ContainSubstring("not found")))
}
