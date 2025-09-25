// This file contains tests for dashboard support functions.
// These tests verify the support functions in dashboard_support.go.
//
//nolint:testpackage
package dashboard

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	createIngressErrorMsg = "Failed to create ingress: %v"
	testNamespace         = "test-namespace"
	dashboardURLKey       = "dashboard-url"
	sectionTitleKey       = "section-title"
)

func TestDefaultManifestInfo(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		platform common.Platform
		validate func(t *testing.T, mi odhtypes.ManifestInfo)
	}{
		{
			name:     "OpenDataHub",
			platform: cluster.OpenDataHub,
			validate: func(t *testing.T, mi odhtypes.ManifestInfo) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(mi.ContextDir).Should(Equal(ComponentName))
				g.Expect(mi.SourcePath).Should(Equal("/odh"))
			},
		},
		{
			name:     "SelfManagedRhoai",
			platform: cluster.SelfManagedRhoai,
			validate: func(t *testing.T, mi odhtypes.ManifestInfo) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(mi.ContextDir).Should(Equal(ComponentName))
				g.Expect(mi.SourcePath).Should(Equal("/rhoai/onprem"))
			},
		},
		{
			name:     "ManagedRhoai",
			platform: cluster.ManagedRhoai,
			validate: func(t *testing.T, mi odhtypes.ManifestInfo) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(mi.ContextDir).Should(Equal(ComponentName))
				g.Expect(mi.SourcePath).Should(Equal("/rhoai/addon"))
			},
		},
		{
			name:     "UnknownPlatform",
			platform: "unknown-platform",
			validate: func(t *testing.T, mi odhtypes.ManifestInfo) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(mi.ContextDir).Should(Equal(ComponentName))
				// Should default to empty SourcePath for unknown platform
				g.Expect(mi.SourcePath).Should(Equal(""))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mi := DefaultManifestInfo(tc.platform)
			tc.validate(t, mi)
		})
	}
}

func TestBffManifestsPath(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	mi := BffManifestsPath()

	g.Expect(mi.ContextDir).Should(Equal(ComponentName))
	g.Expect(mi.SourcePath).Should(Equal("modular-architecture"))
}

func TestComputeKustomizeVariable(t *testing.T) {
	t.Parallel()
	t.Run("SuccessWithOpenDataHub", testComputeKustomizeVariableOpenDataHub)
	t.Run("SuccessWithSelfManagedRhoai", testComputeKustomizeVariableSelfManagedRhoai)
	t.Run("SuccessWithManagedRhoai", testComputeKustomizeVariableManagedRhoai)
	t.Run("DomainRetrievalError", testComputeKustomizeVariableDomainError)
	t.Run("NilDSCISpec", testComputeKustomizeVariableNilDSCISpec)
}

func testComputeKustomizeVariableOpenDataHub(t *testing.T) {
	t.Parallel()
	cli, _ := fakeclient.New()
	ingress := createTestIngress()
	err := cli.Create(t.Context(), ingress)
	if err != nil {
		t.Fatalf(createIngressErrorMsg, err)
	}

	dsciSpec := &dsciv1.DSCInitializationSpec{
		ApplicationsNamespace: testNamespace,
	}

	result, err := ComputeKustomizeVariable(t.Context(), cli, cluster.OpenDataHub, dsciSpec)

	g := NewWithT(t)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(result).Should(HaveKey(dashboardURLKey))
	g.Expect(result).Should(HaveKey(sectionTitleKey))
	g.Expect(result[dashboardURLKey]).Should(Equal("https://odh-dashboard-test-namespace.apps.example.com"))
	g.Expect(result[sectionTitleKey]).Should(Equal("OpenShift Open Data Hub"))
}

func testComputeKustomizeVariableSelfManagedRhoai(t *testing.T) {
	t.Parallel()
	cli, _ := fakeclient.New()
	ingress := createTestIngress()
	err := cli.Create(t.Context(), ingress)
	if err != nil {
		t.Fatalf(createIngressErrorMsg, err)
	}

	dsciSpec := &dsciv1.DSCInitializationSpec{
		ApplicationsNamespace: testNamespace,
	}

	result, err := ComputeKustomizeVariable(t.Context(), cli, cluster.SelfManagedRhoai, dsciSpec)

	g := NewWithT(t)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(result[dashboardURLKey]).Should(Equal("https://rhods-dashboard-test-namespace.apps.example.com"))
	g.Expect(result[sectionTitleKey]).Should(Equal("OpenShift Self Managed Services"))
}

func testComputeKustomizeVariableManagedRhoai(t *testing.T) {
	t.Parallel()
	cli, _ := fakeclient.New()
	ingress := createTestIngress()
	err := cli.Create(t.Context(), ingress)
	if err != nil {
		t.Fatalf(createIngressErrorMsg, err)
	}

	dsciSpec := &dsciv1.DSCInitializationSpec{
		ApplicationsNamespace: testNamespace,
	}

	result, err := ComputeKustomizeVariable(t.Context(), cli, cluster.ManagedRhoai, dsciSpec)

	g := NewWithT(t)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(result[dashboardURLKey]).Should(Equal("https://rhods-dashboard-test-namespace.apps.example.com"))
	g.Expect(result[sectionTitleKey]).Should(Equal("OpenShift Managed Services"))
}

func testComputeKustomizeVariableDomainError(t *testing.T) {
	t.Parallel()
	cli, _ := fakeclient.New()
	// No ingress resource - should cause domain retrieval to fail

	dsciSpec := &dsciv1.DSCInitializationSpec{
		ApplicationsNamespace: testNamespace,
	}

	_, err := ComputeKustomizeVariable(t.Context(), cli, cluster.OpenDataHub, dsciSpec)

	g := NewWithT(t)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("error getting console route URL"))
}

func testComputeKustomizeVariableNilDSCISpec(t *testing.T) {
	t.Parallel()
	cli, _ := fakeclient.New()
	ingress := createTestIngress()
	err := cli.Create(t.Context(), ingress)
	if err != nil {
		t.Fatalf(createIngressErrorMsg, err)
	}

	var computeErr error

	// Handle panic for nil pointer dereference
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Expected panic, convert to error
				computeErr = fmt.Errorf("panic: %v", r)
			}
		}()
		_, computeErr = ComputeKustomizeVariable(t.Context(), cli, cluster.OpenDataHub, nil)
	}()

	g := NewWithT(t)
	g.Expect(computeErr).Should(HaveOccurred())
	g.Expect(computeErr.Error()).Should(ContainSubstring("runtime error: invalid memory address or nil pointer dereference"))
}

func TestComputeComponentName(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		setupRelease   func()
		expectedName   string
		cleanupRelease func()
	}{
		{
			name: "OpenDataHubRelease",
			setupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{Name: cluster.OpenDataHub})
			},
			expectedName: LegacyComponentNameUpstream,
			cleanupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{})
			},
		},
		{
			name: "SelfManagedRhoaiRelease",
			setupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{Name: cluster.SelfManagedRhoai})
			},
			expectedName: LegacyComponentNameDownstream,
			cleanupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{})
			},
		},
		{
			name: "ManagedRhoaiRelease",
			setupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{Name: cluster.ManagedRhoai})
			},
			expectedName: LegacyComponentNameDownstream,
			cleanupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{})
			},
		},
		{
			name: "UnknownRelease",
			setupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{Name: "unknown-release"})
			},
			expectedName: LegacyComponentNameUpstream,
			cleanupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{})
			},
		},
		{
			name: "EmptyRelease",
			setupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{Name: ""})
			},
			expectedName: LegacyComponentNameUpstream,
			cleanupRelease: func() {
				cluster.SetReleaseForTesting(common.Release{})
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.setupRelease()
			defer tc.cleanupRelease()

			result := ComputeComponentName()

			g := NewWithT(t)
			g.Expect(result).Should(Equal(tc.expectedName))
		})
	}
}

func TestComputeComponentNameMultipleCalls(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Test that multiple calls return the same result (deterministic)
	cluster.SetReleaseForTesting(common.Release{Name: cluster.OpenDataHub})
	defer cluster.SetReleaseForTesting(common.Release{})

	result1 := ComputeComponentName()
	result2 := ComputeComponentName()
	result3 := ComputeComponentName()

	g.Expect(result1).Should(Equal(result2))
	g.Expect(result2).Should(Equal(result3))
	g.Expect(result1).Should(Equal(LegacyComponentNameUpstream))
}

// Helper function to create test ingress.
func createTestIngress() *unstructured.Unstructured {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")
	ingress.Object["spec"] = map[string]interface{}{
		"domain": "apps.example.com",
	}
	return ingress
}
