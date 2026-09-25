package aipipelines_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aipipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

func TestPopulatePlatformModule(t *testing.T) {
	h := aipipelines.NewHandler()
	pm := &configv1alpha1.PlatformModules{}
	dsc := &dscv2.DataScienceCluster{}
	dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed

	h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: dsc})

	if pm.AIPipelines.ManagementState != operatorv1.Managed {
		t.Fatalf("expected Managed AIPipelines module, got %q", pm.AIPipelines.ManagementState)
	}
}

func TestPopulatePlatformModuleDefaultsToRemoved(t *testing.T) {
	h := aipipelines.NewHandler()
	pm := &configv1alpha1.PlatformModules{}

	h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: &dscv2.DataScienceCluster{}})

	if pm.AIPipelines.ManagementState != operatorv1.Removed {
		t.Fatalf("expected empty DSC state to become Removed, got %q", pm.AIPipelines.ManagementState)
	}
}

func TestBuildModuleCR(t *testing.T) {
	h := aipipelines.NewHandler()
	dsc := &dscv2.DataScienceCluster{}
	dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIPipelines.ArgoWorkflowsControllers = &componentApi.ArgoWorkflowsControllersSpec{
		ManagementState: operatorv1.Removed,
	}

	moduleCR, err := h.BuildModuleCR(t.Context(), nil, &modules.DSCContext{DSC: dsc}, nil)
	if err != nil {
		t.Fatalf("build AIPipelines CR: %v", err)
	}
	if moduleCR.GetName() != "default-aipipelines" {
		t.Fatalf("unexpected module CR name %q", moduleCR.GetName())
	}
	if moduleCR.GroupVersionKind() != h.GetGVK() {
		t.Fatalf("unexpected module CR GVK %s", moduleCR.GroupVersionKind())
	}
	state, found, err := unstructured.NestedString(moduleCR.Object, "spec", "argoWorkflowsControllers", "managementState")
	if err != nil || !found {
		t.Fatalf("read Argo management state: found=%t err=%v", found, err)
	}
	if state != string(operatorv1.Removed) {
		t.Fatalf("expected Removed Argo controllers, got %q", state)
	}
}

func TestBuildModuleCRDefaultsArgoToManaged(t *testing.T) {
	h := aipipelines.NewHandler()
	moduleCR, err := h.BuildModuleCR(t.Context(), nil, &modules.DSCContext{DSC: &dscv2.DataScienceCluster{}}, nil)
	if err != nil {
		t.Fatalf("build AIPipelines CR: %v", err)
	}
	state, _, _ := unstructured.NestedString(moduleCR.Object, "spec", "argoWorkflowsControllers", "managementState")
	if state != string(operatorv1.Managed) {
		t.Fatalf("expected default Managed Argo controllers, got %q", state)
	}
}

func TestOperatorManifestsAndEnvironment(t *testing.T) {
	h := aipipelines.NewHandler()
	if h.Config.SourcePath != "" {
		t.Fatalf("expected platform-specific overlays without a default source path, got %q", h.Config.SourcePath)
	}
	platform := &modules.PlatformContext{
		ManifestsBasePath: "/opt/manifests",
		Release: common.Release{
			Name: cluster.OpenDataHub,
		},
	}

	manifests := h.GetOperatorManifests(platform)
	if len(manifests.Manifests) != 1 || manifests.Manifests[0].SourcePath != "overlays/odh/dspo" {
		t.Fatalf("unexpected ODH manifests: %#v", manifests.Manifests)
	}
	for _, platformName := range []common.Platform{cluster.SelfManagedRhoai, cluster.ManagedRhoai, cluster.XKS} {
		t.Run(string(platformName), func(t *testing.T) {
			platform.Release.Name = platformName
			manifests := h.GetOperatorManifests(platform)
			if len(manifests.Manifests) != 1 || manifests.Manifests[0].SourcePath != "overlays/rhoai/dspo" {
				t.Fatalf("unexpected manifests for %s: %#v", platformName, manifests.Manifests)
			}
		})
	}
	if got := h.GetExtraEnv()["DSPO_ENABLEAIPIPELINESMODULECONTROLLER"]; got != "true" {
		t.Fatalf("expected module controller handoff flag, got %q", got)
	}
}

func TestWriteDSCComponentStatus(t *testing.T) {
	h := aipipelines.NewHandler()
	dsc := &dscv2.DataScienceCluster{}
	releases := []common.ComponentRelease{{Name: "platform", Version: "3.6.0"}}

	h.WriteDSCComponentStatus(dsc, true, releases)

	if dsc.Status.Components.AIPipelines.ManagementState != operatorv1.Managed {
		t.Fatalf("expected Managed DSC status, got %q", dsc.Status.Components.AIPipelines.ManagementState)
	}
	if dsc.Status.Components.AIPipelines.DataSciencePipelinesCommonStatus == nil ||
		len(dsc.Status.Components.AIPipelines.Releases) != 1 {
		t.Fatalf("expected AIPipelines release status, got %#v", dsc.Status.Components.AIPipelines)
	}
}
