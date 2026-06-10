//nolint:testpackage // Exercises package-private module status wiring directly.
package modules

import (
	"context"
	"testing"

	semver "github.com/blang/semver/v4"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

func withTestRegistry(t *testing.T) {
	t.Helper()

	original := r
	r = &Registry{}
	t.Cleanup(func() {
		r = original
		provision.DefaultRegistry().Reset()
		provision.GetRunlevelTracker().Reset()
	})

	provision.DefaultRegistry().Reset()
	provision.GetRunlevelTracker().Reset()
}

const (
	cleanupInitContainerName       = "copy-manifests"
	testDSCName                    = "test-dsc"
	testDSCIName                   = "default-dsci"
	testApplicationsNamespace      = "opendatahub"
	testProvisioningModuleName     = "test-module"
	testProvisioningModuleKind     = "TestModule"
	testProvisioningModuleGroup    = "test.opendatahub.io"
	testProvisioningModuleVersion  = "v1alpha1"
	testProvisioningOverlayODH     = "overlays/odh"
	testProvisioningVersion        = "2.30.0"
	testProvisioningDeploymentName = "test-module-controller-manager"
	testProvisioningImageEnv       = "RELATED_IMAGE_TEST_MODULE"
	testProvisioningControllerEnv  = "RELATED_IMAGE_TEST_MODULE_CONTROLLER"
)

type removedProjectorStub struct{}

func (removedProjectorStub) GetName() string { return "removed-projector" }

func (removedProjectorStub) IsEnabled(*PlatformContext) bool { return false }

func (removedProjectorStub) GetGVK() schema.GroupVersionKind { return schema.GroupVersionKind{} }

func (removedProjectorStub) GetOperatorManifests(*PlatformContext) OperatorManifests {
	return OperatorManifests{}
}

func (removedProjectorStub) BuildModuleCR(context.Context, client.Client, *PlatformContext) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (removedProjectorStub) GetRelatedImages() []string { return nil }

func (removedProjectorStub) GetModuleStatus(context.Context, client.Client) (*ModuleStatus, error) {
	return &ModuleStatus{}, nil
}

func (removedProjectorStub) GetModuleCRState(context.Context, client.Client) (CRState, error) {
	return CRStateAbsent, nil
}

func (removedProjectorStub) DeleteModuleCR(context.Context, client.Client) error { return nil }

func (removedProjectorStub) DeleteOperatorResources(context.Context, client.Client, *PlatformContext) error {
	return nil
}

func (removedProjectorStub) UpdateDSCComponentStatus(_ context.Context, rr *types.ReconciliationRequest, platform *PlatformContext) (metav1.ConditionStatus, error) {
	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return metav1.ConditionUnknown, nil
	}
	dsc.Status.Components.MLflowOperator.ManagementState = operatorv1.Removed
	rr.Conditions.MarkFalse(
		"MLflowOperatorReady",
		conditions.WithReason(string(platform.DSC.Spec.Components.MLflowOperator.ManagementState)),
		conditions.WithMessage("Component ManagementState is set to %s", string(platform.DSC.Spec.Components.MLflowOperator.ManagementState)),
		conditions.WithSeverity(common.ConditionSeverityInfo),
	)
	return metav1.ConditionFalse, nil
}

func TestProjectDSCCompatibilityStatusProjectsRemovedModulesIntoDSCStatus(t *testing.T) {
	withTestRegistry(t)
	DefaultRegistry().Add(removedProjectorStub{})
	provision.Add("removed-projector", provision.KindModule, dag.RL(20))

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: testDSCName},
	}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Removed
	dsc.Status.Components.MLflowOperator.ManagementState = operatorv1.Managed

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: testDSCIName},
	}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, status.ConditionTypeModulesReady),
	}

	notReady, managed, err := ProjectDSCCompatibilityStatus(context.Background(), rr)
	if err != nil {
		t.Fatalf("project DSC compatibility status: %v", err)
	}
	if managed != 0 {
		t.Fatalf("expected no managed modules for removed projector, got %d", managed)
	}
	if len(notReady) != 1 || notReady[0] != "removed-projector" {
		t.Fatalf("expected removed projector to be reported not ready, got %#v", notReady)
	}

	if got := dsc.Status.Components.MLflowOperator.ManagementState; got != operatorv1.Removed {
		t.Fatalf("expected MLflowOperator DSC status managementState %q, got %q", operatorv1.Removed, got)
	}

	ready := conditions.FindStatusCondition(dsc.GetStatus(), "MLflowOperatorReady")
	if ready == nil {
		t.Fatalf("expected MLflowOperatorReady condition to be projected")
	}
	if ready.Reason != string(operatorv1.Removed) {
		t.Fatalf("expected MLflowOperatorReady reason %q, got %q", operatorv1.Removed, ready.Reason)
	}
}

type deletingCleanupStub struct{}

func (deletingCleanupStub) GetName() string { return "cleanup-module" }

func (deletingCleanupStub) IsEnabled(*PlatformContext) bool { return false }

func (deletingCleanupStub) GetGVK() schema.GroupVersionKind { return schema.GroupVersionKind{} }

func (deletingCleanupStub) GetOperatorManifests(*PlatformContext) OperatorManifests {
	return OperatorManifests{
		Manifests: []types.ManifestInfo{{
			Path:       "cleanup-module",
			SourcePath: "overlays/odh",
		}},
	}
}

func (deletingCleanupStub) BuildModuleCR(context.Context, client.Client, *PlatformContext) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (deletingCleanupStub) GetRelatedImages() []string { return []string{"RELATED_IMAGE_CLEANUP"} }

func (deletingCleanupStub) GetModuleStatus(context.Context, client.Client) (*ModuleStatus, error) {
	return &ModuleStatus{}, nil
}

func (deletingCleanupStub) GetModuleCRState(context.Context, client.Client) (CRState, error) {
	return CRStateDeleting, nil
}

func (deletingCleanupStub) DeleteModuleCR(context.Context, client.Client) error { return nil }

func (deletingCleanupStub) DeleteOperatorResources(context.Context, client.Client, *PlatformContext) error {
	return nil
}

func (deletingCleanupStub) GetDeploymentName() string { return "cleanup-module-controller-manager" }

func (deletingCleanupStub) GetControllerImage() string { return "RELATED_IMAGE_CLEANUP_CONTROLLER" }

func (deletingCleanupStub) GetInitContainerName() string { return cleanupInitContainerName }

func (deletingCleanupStub) GetExtraEnv() map[string]string {
	return map[string]string{"ENABLE_CLEANUP_MODULE_CONTROLLER": "true"}
}

type provisioningModuleStub struct {
	moduleName string
	enabled    bool
	status     *ModuleStatus
}

func (s provisioningModuleStub) GetName() string { return s.moduleName }

func (s provisioningModuleStub) IsEnabled(*PlatformContext) bool { return s.enabled }

func (s provisioningModuleStub) GetGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: testProvisioningModuleGroup, Version: testProvisioningModuleVersion, Kind: testProvisioningModuleKind}
}

func (s provisioningModuleStub) GetOperatorManifests(*PlatformContext) OperatorManifests {
	return OperatorManifests{
		Manifests: []types.ManifestInfo{{
			Path:       s.moduleName,
			SourcePath: testProvisioningOverlayODH,
		}},
	}
}

func (s provisioningModuleStub) BuildModuleCR(context.Context, client.Client, *PlatformContext) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(s.GetGVK())
	u.SetName("default-" + s.moduleName)
	return u, nil
}

func (s provisioningModuleStub) GetRelatedImages() []string {
	return []string{testProvisioningImageEnv}
}

func (s provisioningModuleStub) GetModuleStatus(context.Context, client.Client) (*ModuleStatus, error) {
	if s.status != nil {
		return s.status, nil
	}
	return &ModuleStatus{}, nil
}

func (s provisioningModuleStub) GetModuleCRState(context.Context, client.Client) (CRState, error) {
	return CRStateAlive, nil
}

func (s provisioningModuleStub) DeleteModuleCR(context.Context, client.Client) error { return nil }

func (s provisioningModuleStub) DeleteOperatorResources(context.Context, client.Client, *PlatformContext) error {
	return nil
}

func (s provisioningModuleStub) GetDeploymentName() string {
	return s.moduleName + "-controller-manager"
}

func (s provisioningModuleStub) GetControllerImage() string { return testProvisioningControllerEnv }

func (s provisioningModuleStub) GetInitContainerName() string { return cleanupInitContainerName }

func (s provisioningModuleStub) GetExtraEnv() map[string]string {
	return map[string]string{"ENABLE_TEST_MODULE_CONTROLLER": "true"}
}

func TestCleanupDisabledModulesPreservesModuleEnvInjectionWhileDeleting(t *testing.T) {
	withTestRegistry(t)
	DefaultRegistry().Add(deletingCleanupStub{})
	provision.Add("cleanup-module", provision.KindModule, dag.RL(20))

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: testDSCIName},
	}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client: cli,
	}

	if err := cleanupDisabledModules(context.Background(), rr); err != nil {
		t.Fatalf("cleanup disabled modules: %v", err)
	}

	if len(rr.Manifests) != 1 {
		t.Fatalf("expected deleting module manifests to be kept alive, got %d", len(rr.Manifests))
	}
	if rr.ModuleEnvInjection == nil {
		t.Fatalf("expected module env injection to be preserved for deleting module")
	}
	if rr.ModuleEnvInjection.ApplicationsNamespace != testApplicationsNamespace {
		t.Fatalf("expected applications namespace %q, got %q", testApplicationsNamespace, rr.ModuleEnvInjection.ApplicationsNamespace)
	}
	if len(rr.ModuleEnvInjection.PerModuleImages) != 1 {
		t.Fatalf("expected one deleting module env injection entry, got %d", len(rr.ModuleEnvInjection.PerModuleImages))
	}

	moduleImages := rr.ModuleEnvInjection.PerModuleImages[0]
	if moduleImages.DeploymentName != "cleanup-module-controller-manager" {
		t.Fatalf("expected deployment name %q, got %q", "cleanup-module-controller-manager", moduleImages.DeploymentName)
	}
	if moduleImages.ControllerImage != "RELATED_IMAGE_CLEANUP_CONTROLLER" {
		t.Fatalf("expected controller image env var %q, got %q", "RELATED_IMAGE_CLEANUP_CONTROLLER", moduleImages.ControllerImage)
	}
	if moduleImages.InitContainerName != cleanupInitContainerName {
		t.Fatalf("expected init container name %q, got %q", cleanupInitContainerName, moduleImages.InitContainerName)
	}
	if got := moduleImages.ExtraEnv["ENABLE_CLEANUP_MODULE_CONTROLLER"]; got != "true" {
		t.Fatalf("expected cleanup extra env to be preserved, got %q", got)
	}
	if len(moduleImages.Images) != 1 || moduleImages.Images[0] != "RELATED_IMAGE_CLEANUP" {
		t.Fatalf("expected related image env vars to be preserved, got %#v", moduleImages.Images)
	}
}

func TestProvisionModulesAddsResourcesAndEnvInjection(t *testing.T) {
	withTestRegistry(t)

	handler := provisioningModuleStub{
		moduleName: testProvisioningModuleName,
		enabled:    true,
		status: &ModuleStatus{
			Conditions: []metav1.Condition{{
				Type:   status.ConditionTypeReady,
				Status: metav1.ConditionTrue,
			}},
		},
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))
	provision.Add(handler.GetName(), provision.KindModule, dag.RL(20))

	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName, UID: "uid-1"}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Release:    common.Release{Name: common.Platform("Open Data Hub"), Version: ofversion.OperatorVersion{Version: semver.MustParse(testProvisioningVersion)}},
		Conditions: conditions.NewManager(dsc, status.ConditionTypeModulesReady),
	}

	if err := provisionModules(context.Background(), rr); err != nil {
		t.Fatalf("provision modules: %v", err)
	}

	if len(rr.Resources) != 1 {
		t.Fatalf("expected one projected module CR, got %d", len(rr.Resources))
	}
	if len(rr.Manifests) != 1 {
		t.Fatalf("expected one module manifest entry, got %d", len(rr.Manifests))
	}
	if rr.ModuleEnvInjection == nil || len(rr.ModuleEnvInjection.PerModuleImages) != 1 {
		t.Fatalf("expected one module env injection entry, got %#v", rr.ModuleEnvInjection)
	}
	if rr.ModuleEnvInjection.ApplicationsNamespace != testApplicationsNamespace {
		t.Fatalf("expected applications namespace %q, got %q", testApplicationsNamespace, rr.ModuleEnvInjection.ApplicationsNamespace)
	}
	if rr.ModuleEnvInjection.PerModuleImages[0].DeploymentName != testProvisioningDeploymentName {
		t.Fatalf("expected deployment name to be preserved, got %#v", rr.ModuleEnvInjection.PerModuleImages[0])
	}
}

func TestInjectPlatformConfigCreatesModuleConfigMap(t *testing.T) {
	withTestRegistry(t)

	handler := provisioningModuleStub{moduleName: testProvisioningModuleName, enabled: true}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))

	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
		Release:  common.Release{Name: common.Platform("Open Data Hub"), Version: ofversion.OperatorVersion{Version: semver.MustParse(testProvisioningVersion)}},
	}

	if err := injectPlatformConfig(context.Background(), rr); err != nil {
		t.Fatalf("inject platform config: %v", err)
	}

	if len(rr.Resources) != 1 {
		t.Fatalf("expected one generated platform config, got %d", len(rr.Resources))
	}
	if got := rr.Resources[0].GetName(); got != "odh-test-module-config" {
		t.Fatalf("expected generated configmap name %q, got %q", "odh-test-module-config", got)
	}
	data, found, err := unstructured.NestedStringMap(rr.Resources[0].Object, "data")
	if err != nil || !found {
		t.Fatalf("expected configmap data to be present, got found=%v err=%v", found, err)
	}
	if data[PlatformVersionKey] != testProvisioningVersion {
		t.Fatalf("expected platform version %q, got %#v", testProvisioningVersion, data)
	}
}

func TestComputeModulesStatusMarksNotReadyModules(t *testing.T) {
	withTestRegistry(t)

	readyHandler := provisioningModuleStub{
		moduleName: "ready-module",
		enabled:    true,
		status: &ModuleStatus{Conditions: []metav1.Condition{{
			Type:   status.ConditionTypeReady,
			Status: metav1.ConditionTrue,
		}}},
	}
	notReadyHandler := provisioningModuleStub{
		moduleName: "not-ready-module",
		enabled:    true,
		status:     &ModuleStatus{},
	}
	DefaultRegistry().Add(readyHandler, WithRunlevel(dag.RL(20)))
	DefaultRegistry().Add(notReadyHandler, WithRunlevel(dag.RL(20)))

	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditions.NewManager(dsc, status.ConditionTypeModulesReady),
	}

	if err := ComputeModulesStatus(context.Background(), rr); err != nil {
		t.Fatalf("compute modules status: %v", err)
	}

	ready := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if ready == nil {
		t.Fatalf("expected ModulesReady condition to be set")
	}
	if ready.Status != metav1.ConditionFalse || ready.Reason != status.NotReadyReason {
		t.Fatalf("expected ModulesReady=False/%q, got %#v", status.NotReadyReason, ready)
	}
	if ready.Message == "" {
		t.Fatalf("expected ModulesReady message to mention the not ready module")
	}
}
