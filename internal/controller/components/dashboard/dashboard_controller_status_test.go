// This file contains tests for dashboard controller status update functionality.
// These tests verify the dashboard.UpdateStatus function and related status logic.
package dashboard_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// mockClient implements client.Client interface for testing.
type mockClient struct {
	client.Client

	listError error
}

func (m *mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// Check if this is a Route list call by examining the concrete type
	if _, isRouteList := list.(*routev1.RouteList); isRouteList && m.listError != nil {
		return m.listError
	}
	return m.Client.List(ctx, list, opts...)
}

func TestUpdateStatusNoRoutes(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create DSCI resource
	dsci := CreateTestDSCI(TestNamespace)
	err = cli.Create(ctx, dsci)
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := CreateTestDashboard()

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
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

	// Create DSCI resource
	dsci := CreateTestDSCI(TestNamespace)
	err = cli.Create(ctx, dsci)
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := CreateTestDashboard()

	// Create a route with the expected label
	route := createRoute("odh-dashboard", TestRouteHost, true)

	err = cli.Create(ctx, route)
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
	}

	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dashboardInstance.Status.URL).Should(Equal("https://" + TestRouteHost))
}

func TestUpdateStatusInvalidInstance(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: &componentApi.Kserve{}, // Wrong type
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

	// Create DSCI resource
	dsci := CreateTestDSCI(TestNamespace)
	err = cli.Create(ctx, dsci)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create multiple routes with the same label
	route1 := createRouteWithLabels("odh-dashboard-1", "odh-dashboard-1-test-namespace.apps.example.com", true, map[string]string{
		labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
	})

	route2 := createRouteWithLabels("odh-dashboard-2", "odh-dashboard-2-test-namespace.apps.example.com", true, map[string]string{
		labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
	})

	err = cli.Create(ctx, route1)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, route2)
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := CreateTestDashboard()

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
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

	// Create DSCI resource
	dsci := CreateTestDSCI(TestNamespace)
	err = cli.Create(ctx, dsci)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a route without ingress status
	route := createRouteWithLabels("odh-dashboard", "odh-dashboard-test-namespace.apps.example.com", false, map[string]string{
		labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
	})

	err = cli.Create(ctx, route)
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := CreateTestDashboard()

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
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

	// Create DSCI resource
	dsci := CreateTestDSCI(TestNamespace)
	err = baseCli.Create(ctx, dsci)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Inject a List error for Route objects to simulate a failing route list operation
	mockCli := &mockClient{
		Client:    baseCli,
		listError: errors.New("failed to list routes"),
	}

	dashboardInstance := CreateTestDashboard()

	rr := &odhtypes.ReconciliationRequest{
		Client:   mockCli,
		Instance: dashboardInstance,
	}

	// Test the case where list fails
	err = dashboard.UpdateStatus(ctx, rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("failed to list routes"))
}
