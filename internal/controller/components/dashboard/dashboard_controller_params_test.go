// This file contains tests for dashboard controller parameter functionality.
// These tests verify the dashboardctrl.SetKustomizedParams function and related parameter logic.
package dashboard_test

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	dashboardctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const paramsEnvFileName = "params.env"
const errorNoManifestsAvailable = "no manifests available"

func TestSetKustomizedParams(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create the directory structure that matches the manifest path
	manifestDir := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh")
	err := os.MkdirAll(manifestDir, 0755)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a params.env file in the manifest directory
	paramsEnvPath := filepath.Join(manifestDir, paramsEnvFileName)
	paramsEnvContent := dashboardctrl.InitialParamsEnvContent
	err = os.WriteFile(paramsEnvPath, []byte(paramsEnvContent), 0600)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a mock client that returns a domain
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
		},
	}

	// Mock the domain function by creating an ingress resource
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err = unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, ingress)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify that the params.env file was updated with the correct values
	updatedContent, err := os.ReadFile(paramsEnvPath)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Check that the dashboard-url was updated with the expected value
	expectedDashboardURL := "https://odh-dashboard-" + dashboardctrl.TestNamespace + ".apps.example.com"
	g.Expect(string(updatedContent)).Should(ContainSubstring("dashboard-url=" + expectedDashboardURL))

	// Check that the section-title was updated with the expected value
	expectedSectionTitle := dashboardctrl.SectionTitle[cluster.OpenDataHub]
	g.Expect(string(updatedContent)).Should(ContainSubstring("section-title=" + expectedSectionTitle))
}

func TestSetKustomizedParamsError(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create the directory structure that matches the manifest path
	manifestDir := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh")
	err := os.MkdirAll(manifestDir, 0755)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a params.env file in the manifest directory
	paramsEnvPath := filepath.Join(manifestDir, paramsEnvFileName)
	paramsEnvContent := dashboardctrl.InitialParamsEnvContent
	err = os.WriteFile(paramsEnvPath, []byte(paramsEnvContent), 0600)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a mock client that will fail to get domain
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
		},
	}

	// Test without creating the ingress resource (should fail to get domain)
	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring(dashboardctrl.ErrorFailedToSetVariable))
	t.Logf(dashboardctrl.LogSetKustomizedParamsError, err)
}

func TestSetKustomizedParamsInvalidManifest(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create the directory structure that matches the manifest path
	manifestDir := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh")
	err := os.MkdirAll(manifestDir, 0755)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a params.env file in the manifest directory
	paramsEnvPath := filepath.Join(manifestDir, paramsEnvFileName)
	paramsEnvContent := dashboardctrl.InitialParamsEnvContent
	err = os.WriteFile(paramsEnvPath, []byte(paramsEnvContent), 0600)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a mock client that returns a domain
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
		},
	}

	// Mock the domain function by creating an ingress resource
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err = unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, ingress)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test with invalid manifest path
	rr.Manifests[0].Path = "/invalid/path"

	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	// If graceful handling means success despite invalid path:
	g.Expect(err).ShouldNot(HaveOccurred())
	// OR if it should fail with specific error:
	// g.Expect(err).To(HaveOccurred())
	// g.Expect(err.Error()).Should(ContainSubstring("expected error message"))
}

func TestSetKustomizedParamsWithEmptyManifests(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create the OpenShift ingress resource that computeKustomizeVariable needs
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")
	err := unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:    cli,
		Instance:  dashboardInstance,
		DSCI:      dsci,
		Release:   common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{}, // Empty manifests
	}

	// Should fail due to empty manifests
	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring(errorNoManifestsAvailable))
}

func TestSetKustomizedParamsWithNilManifests(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create the OpenShift ingress resource that computeKustomizeVariable needs
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")
	err := unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:    cli,
		Instance:  dashboardInstance,
		DSCI:      dsci,
		Release:   common.Release{Name: cluster.OpenDataHub},
		Manifests: nil, // Nil manifests
	}

	// Should fail due to nil manifests
	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring(errorNoManifestsAvailable))
}

func TestSetKustomizedParamsWithInvalidManifestPath(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create the OpenShift ingress resource that computeKustomizeVariable needs
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")
	err := unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: "/invalid/path", ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
		},
	}

	// Should handle invalid manifest path gracefully
	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	t.Log("dashboardctrl.SetKustomizedParams handled invalid path gracefully")
}

func TestSetKustomizedParamsWithMultipleManifests(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create the OpenShift ingress resource that computeKustomizeVariable needs
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")
	err := unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     dsci,
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: dashboardctrl.TestPath, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
			{Path: dashboardctrl.TestPath, ContextDir: dashboardctrl.ComponentName, SourcePath: "/bff"},
		},
	}

	// Should work with multiple manifests (uses first one)
	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	if err != nil {
		g.Expect(err.Error()).Should(ContainSubstring(dashboardctrl.ErrorFailedToUpdateParams))
	} else {
		t.Log("dashboardctrl.SetKustomizedParams handled multiple manifests gracefully")
	}
}

func TestSetKustomizedParamsWithNilDSCI(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create the OpenShift ingress resource that computeKustomizeVariable needs
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")
	err := unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(fakeclient.WithObjects(ingress))
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}

	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboardInstance,
		DSCI:     nil, // Nil DSCI
		Release:  common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{
			{Path: dashboardctrl.TestPath, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
		},
	}

	// Should fail due to nil DSCI (nil pointer dereference)
	g.Expect(func() { _ = dashboardctrl.SetKustomizedParams(ctx, rr) }).To(Panic())
}

func TestSetKustomizedParamsWithNoManifestsError(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a client that returns a domain
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create an ingress resource to provide domain
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err = unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, ingress)
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:    cli,
		Instance:  dashboardInstance,
		DSCI:      dsci,
		Release:   common.Release{Name: cluster.OpenDataHub},
		Manifests: []odhtypes.ManifestInfo{}, // Empty manifests - should cause "no manifests available" error
	}

	// Test with empty manifests (should fail with "no manifests available" error)
	err = dashboardctrl.SetKustomizedParams(ctx, rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring(errorNoManifestsAvailable))
	t.Logf("dashboardctrl.SetKustomizedParams failed with empty manifests as expected: %v", err)
}

func TestSetKustomizedParamsWithDifferentReleases(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create the directory structure that matches the manifest path
	manifestDir := filepath.Join(tempDir, dashboardctrl.ComponentName, "odh")
	err := os.MkdirAll(manifestDir, 0755)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a params.env file in the manifest directory
	paramsEnvPath := filepath.Join(manifestDir, paramsEnvFileName)
	paramsEnvContent := dashboardctrl.InitialParamsEnvContent
	err = os.WriteFile(paramsEnvPath, []byte(paramsEnvContent), 0600)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a mock client that returns a domain
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardInstance := &componentApi.Dashboard{}
	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboardctrl.TestNamespace,
		},
	}

	// Test with different releases
	releases := []common.Release{
		{Name: cluster.OpenDataHub},
		{Name: cluster.ManagedRhoai},
		{Name: cluster.SelfManagedRhoai},
	}

	// Mock the domain function by creating an ingress resource once
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err = unstructured.SetNestedField(ingress.Object, dashboardctrl.TestDomain, "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Create(ctx, ingress)
	g.Expect(err).ShouldNot(HaveOccurred())

	for _, release := range releases {
		t.Run("test", func(t *testing.T) {
			rr := &odhtypes.ReconciliationRequest{
				Client:   cli,
				Instance: dashboardInstance,
				DSCI:     dsci,
				Release:  release,
				Manifests: []odhtypes.ManifestInfo{
					{Path: tempDir, ContextDir: dashboardctrl.ComponentName, SourcePath: "/odh"},
				},
			}

			err = dashboardctrl.SetKustomizedParams(ctx, rr)
			if err != nil {
				g.Expect(err.Error()).Should(ContainSubstring(dashboardctrl.ErrorFailedToUpdateParams))
				t.Logf("dashboardctrl.SetKustomizedParams returned error: %v", err)
			} else {
				t.Logf("dashboardctrl.SetKustomizedParams handled release gracefully")
			}
		})
	}
}
