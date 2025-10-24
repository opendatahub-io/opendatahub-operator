package dashboard_test

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	dashboardURLKey = "dashboard-url"
	sectionTitleKey = "section-title"
	testNamespace   = "test-namespace"

	// Test constants for platform names.
	openDataHubPlatformName      = "OpenDataHub platform"
	selfManagedRhoaiPlatformName = "SelfManagedRhoai platform"
	managedRhoaiPlatformName     = "ManagedRhoai platform"
	unsupportedPlatformName      = "Unsupported platform"
	unsupportedPlatformValue     = "unsupported-platform"
	unsupportedPlatformErrorMsg  = "unsupported platform"
)

func TestDefaultManifestInfo(t *testing.T) {
	tests := []struct {
		name     string
		platform common.Platform
		expected string
	}{
		{
			name:     openDataHubPlatformName,
			platform: cluster.OpenDataHub,
			expected: "/odh",
		},
		{
			name:     selfManagedRhoaiPlatformName,
			platform: cluster.SelfManagedRhoai,
			expected: "/rhoai/onprem",
		},
		{
			name:     managedRhoaiPlatformName,
			platform: cluster.ManagedRhoai,
			expected: "/rhoai/addon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			manifestInfo, err := dashboard.DefaultManifestInfo(tt.platform)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(manifestInfo.ContextDir).Should(Equal(dashboard.ComponentName))
			g.Expect(manifestInfo.SourcePath).Should(Equal(tt.expected))
		})
	}
}

func TestBffManifestsPath(t *testing.T) {
	g := NewWithT(t)
	manifestInfo := dashboard.BffManifestsPath()
	g.Expect(manifestInfo.ContextDir).Should(Equal(dashboard.ComponentName))
	g.Expect(manifestInfo.SourcePath).Should(Equal(dashboard.ModularArchitectureSourcePath))
}

func TestComputeKustomizeVariable(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a mock client with a console route
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "console",
			Namespace: "openshift-console",
		},
		Spec: routev1.RouteSpec{
			Host: "console-openshift-console.apps.example.com",
		},
	}

	// Create the OpenShift ingress resource that cluster.GetDomain() needs
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err := unstructured.SetNestedField(ingress.Object, "apps.example.com", "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(fakeclient.WithObjects(route, ingress))
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := createDSCI()

	tests := []struct {
		name     string
		platform common.Platform
		expected map[string]string
	}{
		{
			name:     "OpenDataHub platform",
			platform: cluster.OpenDataHub,
			expected: map[string]string{
				dashboardURLKey: "https://odh-dashboard-test-namespace.apps.example.com",
				sectionTitleKey: "OpenShift Open Data Hub",
			},
		},
		{
			name:     "SelfManagedRhoai platform",
			platform: cluster.SelfManagedRhoai,
			expected: map[string]string{
				dashboardURLKey: "https://rhods-dashboard-test-namespace.apps.example.com",
				sectionTitleKey: "OpenShift Self Managed Services",
			},
		},
		{
			name:     "ManagedRhoai platform",
			platform: cluster.ManagedRhoai,
			expected: map[string]string{
				dashboardURLKey: "https://rhods-dashboard-test-namespace.apps.example.com",
				sectionTitleKey: "OpenShift Managed Services",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			variables, err := dashboard.ComputeKustomizeVariable(ctx, cli, tt.platform, &dsci.Spec)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(variables).Should(Equal(tt.expected))
		})
	}
}

func TestComputeKustomizeVariableNoConsoleRoute(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := createDSCI()

	_, err = dashboard.ComputeKustomizeVariable(ctx, cli, cluster.OpenDataHub, &dsci.Spec)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("error getting console route URL"))
}

func TestInit(t *testing.T) {
	handler := &dashboard.ComponentHandler{}

	// Test successful initialization for different platforms
	platforms := []common.Platform{
		cluster.OpenDataHub,
		cluster.SelfManagedRhoai,
		cluster.ManagedRhoai,
	}

	for _, platform := range platforms {
		t.Run(string(platform), func(t *testing.T) {
			g := NewWithT(t)
			err := handler.Init(platform)
			g.Expect(err).ShouldNot(HaveOccurred())
		})
	}
}

func TestGetSectionTitle(t *testing.T) {
	runPlatformTest(t, "GetSectionTitle", dashboard.GetSectionTitle)
}

func TestGetBaseConsoleURL(t *testing.T) {
	runPlatformTest(t, "GetBaseConsoleURL", dashboard.GetBaseConsoleURL)
}

func TestGetOverlaysSourcePath(t *testing.T) {
	runPlatformTest(t, "GetOverlaysSourcePath", dashboard.GetOverlaysSourcePath)
}

// runPlatformTest is a helper function that reduces code duplication in platform testing.
func runPlatformTest(t *testing.T, testName string, testFunc func(platform common.Platform) (string, error)) {
	t.Helper()

	tests := []struct {
		name     string
		platform common.Platform
		expected string
		hasError bool
	}{
		{
			name:     openDataHubPlatformName,
			platform: cluster.OpenDataHub,
			expected: getExpectedValue(testName, cluster.OpenDataHub),
			hasError: false,
		},
		{
			name:     selfManagedRhoaiPlatformName,
			platform: cluster.SelfManagedRhoai,
			expected: getExpectedValue(testName, cluster.SelfManagedRhoai),
			hasError: false,
		},
		{
			name:     managedRhoaiPlatformName,
			platform: cluster.ManagedRhoai,
			expected: getExpectedValue(testName, cluster.ManagedRhoai),
			hasError: false,
		},
		{
			name:     unsupportedPlatformName,
			platform: unsupportedPlatformValue,
			expected: "",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := testFunc(tt.platform)
			if tt.hasError {
				g.Expect(err).Should(HaveOccurred())
				g.Expect(err.Error()).Should(ContainSubstring(unsupportedPlatformErrorMsg))
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(result).Should(Equal(tt.expected))
			}
		})
	}
}

// getExpectedValue returns the expected value for a given test name and platform.
func getExpectedValue(testName string, platform common.Platform) string {
	switch testName {
	case "GetSectionTitle":
		switch platform {
		case cluster.OpenDataHub:
			return "OpenShift Open Data Hub"
		case cluster.SelfManagedRhoai:
			return "OpenShift Self Managed Services"
		case cluster.ManagedRhoai:
			return "OpenShift Managed Services"
		}
	case "GetBaseConsoleURL":
		switch platform {
		case cluster.OpenDataHub:
			return "https://odh-dashboard-"
		case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
			return "https://rhods-dashboard-"
		}
	case "GetOverlaysSourcePath":
		switch platform {
		case cluster.OpenDataHub:
			return "/odh"
		case cluster.SelfManagedRhoai:
			return "/rhoai/onprem"
		case cluster.ManagedRhoai:
			return "/rhoai/addon"
		}
	}
	return ""
}
