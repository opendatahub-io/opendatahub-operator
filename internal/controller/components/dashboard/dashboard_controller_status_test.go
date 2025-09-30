// This file contains tests for dashboard controller status update functionality.
// These tests verify the dashboard.UpdateStatus function and related status logic.
package dashboard_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard/dashboard_test"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// MockClient implements client.Client interface for testing.
type MockClient struct {
	client.Client
	listError error
}

func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if m.listError != nil {
		return m.listError
	}
	return m.Client.List(ctx, list, opts...)
}

func TestUpdateStatusNoRoutes(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard_test.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
	}

	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dashboardInstance.Status.URL).Should(BeEmpty())
}

func TestUpdateStatusWithRoute(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard_test.TestNamespace,
		},
	}

	// Create a route with the expected label
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-dashboard",
			Namespace: dashboard_test.TestNamespace,
			Labels: map[string]string{
				"platform.opendatahub.io/part-of": "dashboard",
			},
		},
		Spec: routev1.RouteSpec{
			Host: dashboard_test.TestRouteHost,
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host: dashboard_test.TestRouteHost,
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:   routev1.RouteAdmitted,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
	}

	err = cli.Create(ctx, route)
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
	}

	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dashboardInstance.Status.URL).Should(Equal("https://" + dashboard_test.TestRouteHost))
}

func TestUpdateStatusInvalidInstance(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: &componentApi.Kserve{}, // Wrong type
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: "test-namespace",
			},
		},
	}

	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("is not of type *componentApi.Dashboard"))
}

func TestUpdateStatusWithMultipleRoutes(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create multiple routes with the same label
	route1 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-dashboard-1",
			Namespace: dashboard_test.TestNamespace,
			Labels: map[string]string{
				labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
			},
		},
		Spec: routev1.RouteSpec{
			Host: "odh-dashboard-1-test-namespace.apps.example.com",
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host: "odh-dashboard-1-test-namespace.apps.example.com",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:   routev1.RouteAdmitted,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
	}

	route2 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-dashboard-2",
			Namespace: dashboard_test.TestNamespace,
			Labels: map[string]string{
				labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
			},
		},
		Spec: routev1.RouteSpec{
			Host: "odh-dashboard-2-test-namespace.apps.example.com",
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host: "odh-dashboard-2-test-namespace.apps.example.com",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:   routev1.RouteAdmitted,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
	}

	err = cli.Create(ctx, route1)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, route2)
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard_test.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
	}

	// When there are multiple routes, the URL should be empty
	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dashboardInstance.Status.URL).Should(Equal(""))
}

func TestUpdateStatusWithRouteNoIngress(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a route without ingress status
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-dashboard",
			Namespace: dashboard_test.TestNamespace,
			Labels: map[string]string{
				labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
			},
		},
		Spec: routev1.RouteSpec{
			Host: "odh-dashboard-test-namespace.apps.example.com",
		},
		// No Status.Ingress - this should result in empty URL
	}

	err = cli.Create(ctx, route)
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard_test.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
	}

	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dashboardInstance.Status.URL).Should(Equal(""))
}

func TestUpdateStatusListError(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a fake client as the base
	baseCli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a mock client that intentionally injects a List error for Route objects to simulate a failing
	// route-list operation and verify dashboard.UpdateStatus returns that error (including the expected error substring
	// "failed to list routes")
	mockCli := &MockClient{
		Client:    baseCli,
		listError: errors.New("failed to list routes"),
	}

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard_test.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   mockCli,
		Instance: dashboardInstance,
		DSCI:     dsci,
	}

	// Test the case where list fails
	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("failed to list routes"))
}
