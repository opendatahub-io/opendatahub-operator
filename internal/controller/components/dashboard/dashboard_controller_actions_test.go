//nolint:testpackage
package dashboard

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestMigrateHardwareProfiles(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardHardwareProfileListGVK := schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "HardwareProfileList",
	}

	fakeSchema.AddKnownTypeWithName(gvk.DashboardHardwareProfile, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(dashboardHardwareProfileListGVK, &unstructured.UnstructuredList{})
	fakeSchema.AddKnownTypeWithName(gvk.HardwareProfile, &infrav1.HardwareProfile{})
	fakeSchema.AddKnownTypeWithName(gvk.HardwareProfile.GroupVersion().WithKind("HardwareProfileList"), &infrav1.HardwareProfileList{})

	// Create a CRD for Dashboard HardwareProfile to make HasCRD check pass
	dashboardHWPCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{
			Name: "hardwareprofiles.dashboard.opendatahub.io",
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			StoredVersions: []string{gvk.DashboardHardwareProfile.Version},
		},
	}

	mockDashboardHardwareProfile := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "dashboard.opendatahub.io/v1alpha1",
			"kind":       "HardwareProfile",
			"metadata": map[string]any{
				"name":      "test-name",
				"namespace": "test-namespace",
			},
			"spec": map[string]any{
				"displayName":  "Test Display Name",
				"enabled":      true,
				"description":  "Test Description",
				"tolerations":  []any{},
				"nodeSelector": map[string]any{},
				"identifiers":  []any{},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(mockDashboardHardwareProfile, dashboardHWPCRD),
		fakeclient.WithScheme(fakeSchema),
	)
	g.Expect(err).ShouldNot(HaveOccurred())
	rr := &types.ReconciliationRequest{
		Client: cli,
	}

	err = reconcileHardwareProfiles(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	var createdInfraHWProfile infrav1.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{
		Name:      "test-name",
		Namespace: "test-namespace",
	}, &createdInfraHWProfile)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(createdInfraHWProfile.Name).Should(Equal("test-name"))
	g.Expect(createdInfraHWProfile.Namespace).Should(Equal("test-namespace"))
	g.Expect(createdInfraHWProfile.Spec.SchedulingSpec.SchedulingType).Should(Equal(infrav1.NodeScheduling))
	g.Expect(createdInfraHWProfile.GetAnnotations()["opendatahub.io/display-name"]).Should(Equal("Test Display Name"))
	g.Expect(createdInfraHWProfile.GetAnnotations()["opendatahub.io/description"]).Should(Equal("Test Description"))
	g.Expect(createdInfraHWProfile.GetAnnotations()["opendatahub.io/disabled"]).Should(Equal("false"))
}

func TestCreateInfraHardwareProfile(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	fakeSchema.AddKnownTypeWithName(gvk.HardwareProfile, &infrav1.HardwareProfile{})
	fakeSchema.AddKnownTypeWithName(gvk.HardwareProfile.GroupVersion().WithKind("HardwareProfileList"), &infrav1.HardwareProfileList{})
	cli, err := fakeclient.New(
		fakeclient.WithObjects(),
		fakeclient.WithScheme(fakeSchema),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client: cli,
	}

	logger := log.FromContext(ctx)

	mockDashboardHardwareProfile := &DashboardHardwareProfile{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-namespace",
		},
		Spec: DashboardHardwareProfileSpec{
			DisplayName:  "Test Display Name",
			Enabled:      true,
			Description:  "Test Description",
			Tolerations:  nil,
			NodeSelector: nil,
			Identifiers:  nil,
		},
	}

	var receivedHardwareProfile infrav1.HardwareProfile

	err = createInfraHWP(ctx, rr, logger, mockDashboardHardwareProfile)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Get(ctx, client.ObjectKey{
		Name:      "test-name",
		Namespace: "test-namespace",
	}, &receivedHardwareProfile)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(receivedHardwareProfile.Name).Should(Equal("test-name"))
	g.Expect(receivedHardwareProfile.Namespace).Should(Equal("test-namespace"))
	g.Expect(receivedHardwareProfile.GetAnnotations()["opendatahub.io/display-name"]).Should(Equal("Test Display Name"))
	g.Expect(receivedHardwareProfile.GetAnnotations()["opendatahub.io/description"]).Should(Equal("Test Description"))
	g.Expect(receivedHardwareProfile.GetAnnotations()["opendatahub.io/disabled"]).Should(Equal("false"))
}

func TestDeployObservabilityManifests_WithPersesCRD(t *testing.T) {
	tests := []struct {
		name     string
		platform common.Platform
	}{
		{
			name:     "RHOAI platform attempts deployment when CRD exists",
			platform: cluster.SelfManagedRhoai,
		},
		{
			name:     "ODH platform attempts deployment when CRD exists",
			platform: cluster.OpenDataHub,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			g := NewWithT(t)

			fakeSchema, err := scheme.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			// Register PersesDashboard GVK with the schema so the REST mapper knows about it
			persesDashboardListGVK := schema.GroupVersionKind{
				Group:   "perses.dev",
				Version: "v1alpha1",
				Kind:    "PersesDashboardList",
			}
			fakeSchema.AddKnownTypeWithName(gvk.PersesDashboard, &unstructured.Unstructured{})
			fakeSchema.AddKnownTypeWithName(persesDashboardListGVK, &unstructured.UnstructuredList{})

			// Create a CRD for PersesDashboard to make HasCRD check pass
			persesDashboardCRD := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{
					Name: "persesdashboards.perses.dev",
				},
				Status: apiextensionsv1.CustomResourceDefinitionStatus{
					StoredVersions: []string{"v1alpha1"},
				},
			}

			cli, err := fakeclient.New(
				fakeclient.WithObjects(persesDashboardCRD),
				fakeclient.WithScheme(fakeSchema),
			)
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Client:    cli,
				Instance:  &componentApi.Dashboard{},
				Release:   common.Release{Name: tt.platform},
				Manifests: []types.ManifestInfo{},
			}

			// This test verifies the function attempts to deploy when CRD exists.
			// In test environment, DeployManifestsFromPath will fail because manifest files don't exist.
			// This is expected - the important thing is that the function reaches the deploy call.
			err = deployObservabilityManifests(ctx, rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("failed to deploy observability manifests"))
		})
	}
}

func TestDeployObservabilityManifests_WithoutPersesCRD(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(
		fakeclient.WithScheme(fakeSchema),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:  cli,
		Release: common.Release{Name: cluster.SelfManagedRhoai},
	}

	// When PersesDashboard CRD doesn't exist, function should return early without error
	err = deployObservabilityManifests(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestDeployObservabilityManifests_SkippedForEmptyMonitoringNamespace(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Register PersesDashboard GVK with the schema
	persesDashboardListGVK := schema.GroupVersionKind{
		Group:   "perses.dev",
		Version: "v1alpha1",
		Kind:    "PersesDashboardList",
	}
	fakeSchema.AddKnownTypeWithName(gvk.PersesDashboard, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(persesDashboardListGVK, &unstructured.UnstructuredList{})

	// Create a CRD for PersesDashboard to make HasCRD check pass
	persesDashboardCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{
			Name: "persesdashboards.perses.dev",
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			StoredVersions: []string{"v1alpha1"},
		},
	}

	// Create a DSCI with empty monitoring namespace
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: v1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			Monitoring: serviceApi.DSCIMonitoring{
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: "", // Empty monitoring namespace
				},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(persesDashboardCRD, dsci),
		fakeclient.WithScheme(fakeSchema),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:  cli,
		Release: common.Release{Name: cluster.SelfManagedRhoai},
	}

	// When monitoring namespace is empty, function should return early without error
	err = deployObservabilityManifests(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}
