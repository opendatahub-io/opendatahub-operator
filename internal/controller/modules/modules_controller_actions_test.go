//nolint:testpackage // Exercises package-private module status wiring directly.
package modules

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	semver "github.com/blang/semver/v4"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
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

type deletingCleanupStub struct{}

func (deletingCleanupStub) GetName() string { return "cleanup-module" }

func (deletingCleanupStub) IsEnabled(*configv1alpha1.PlatformModules) bool { return false }

func (deletingCleanupStub) GetGVK() schema.GroupVersionKind { return schema.GroupVersionKind{} }

func (deletingCleanupStub) GetOperatorManifests(*PlatformContext) OperatorManifests {
	return OperatorManifests{
		Manifests: []types.ManifestInfo{{
			Path:       "cleanup-module",
			SourcePath: "overlays/odh",
		}},
	}
}

func (deletingCleanupStub) PopulatePlatformModule(_ *configv1alpha1.PlatformModules, _ *DSCContext) {
}

func (deletingCleanupStub) BuildModuleCR(context.Context, client.Client, *DSCContext, *ModuleCRConfig) (*unstructured.Unstructured, error) {
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

func (deletingCleanupStub) WriteDSCComponentStatus(*dscv2.DataScienceCluster, bool, []common.ComponentRelease) {
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
	submodules []SubmoduleCondition
}

func (s provisioningModuleStub) GetSubmoduleConditions() []SubmoduleCondition {
	return s.submodules
}

func (s provisioningModuleStub) GetName() string { return s.moduleName }

func (s provisioningModuleStub) IsEnabled(*configv1alpha1.PlatformModules) bool { return s.enabled }

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

func (s provisioningModuleStub) PopulatePlatformModule(_ *configv1alpha1.PlatformModules, _ *DSCContext) {
}

func (s provisioningModuleStub) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
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

func (s provisioningModuleStub) WriteDSCComponentStatus(*dscv2.DataScienceCluster, bool, []common.ComponentRelease) {
}

func (s provisioningModuleStub) GetDeploymentName() string {
	return s.moduleName + "-controller-manager"
}

func (s provisioningModuleStub) GetControllerImage() string { return testProvisioningControllerEnv }

func (s provisioningModuleStub) GetInitContainerName() string { return cleanupInitContainerName }

func (s provisioningModuleStub) GetExtraEnv() map[string]string {
	return map[string]string{"ENABLE_TEST_MODULE_CONTROLLER": "true"}
}

type legacyStatusFieldsWriterStub struct {
	provisioningModuleStub

	getStatusErr error
}

func (s legacyStatusFieldsWriterStub) GetModuleStatus(ctx context.Context, cli client.Client) (*ModuleStatus, error) {
	if s.getStatusErr != nil {
		return nil, s.getStatusErr
	}
	return s.provisioningModuleStub.GetModuleStatus(ctx, cli)
}

func (s legacyStatusFieldsWriterStub) WriteLegacyStatusFields(
	_ context.Context,
	_ client.Client,
	dsc *dscv2.DataScienceCluster,
	enabled bool,
) error {
	if dsc == nil {
		return nil
	}
	if !enabled {
		if dsc.Status.Components.Workbenches.WorkbenchesCommonStatus != nil {
			dsc.Status.Components.Workbenches.WorkbenchNamespace = ""
		}
		return nil
	}
	if dsc.Status.Components.Workbenches.WorkbenchesCommonStatus == nil {
		dsc.Status.Components.Workbenches.WorkbenchesCommonStatus = &componentApi.WorkbenchesCommonStatus{}
	}
	if dsc.Status.Components.Workbenches.WorkbenchNamespace == "" {
		dsc.Status.Components.Workbenches.WorkbenchNamespace = dsc.Spec.Components.Workbenches.WorkbenchNamespace
	}
	return nil
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
		Client:   cli,
		Instance: &configv1alpha1.Platform{},
	}

	if err := cleanupDisabledModules(context.Background(), rr); err != nil {
		t.Fatalf("cleanup disabled modules: %v", err)
	}

	if len(rr.Manifests) != 1 {
		t.Fatalf("expected deleting module manifests to be kept alive, got %d", len(rr.Manifests))
	}
	mei := types.GetModuleEnvInjection(rr)
	if mei == nil {
		t.Fatalf("expected module env injection to be preserved for deleting module")
	}
	if mei.ApplicationsNamespace != testApplicationsNamespace {
		t.Fatalf("expected applications namespace %q, got %q", testApplicationsNamespace, mei.ApplicationsNamespace)
	}
	if len(mei.PerModuleImages) != 1 {
		t.Fatalf("expected one deleting module env injection entry, got %d", len(mei.PerModuleImages))
	}

	moduleImages := mei.PerModuleImages[0]
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
			Conditions: []common.Condition{{
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

	if len(rr.Manifests) != 1 {
		t.Fatalf("expected one module manifest entry, got %d", len(rr.Manifests))
	}
	mei2 := types.GetModuleEnvInjection(rr)
	if mei2 == nil || len(mei2.PerModuleImages) != 1 {
		t.Fatalf("expected one module env injection entry, got %#v", mei2)
	}
	if mei2.ApplicationsNamespace != testApplicationsNamespace {
		t.Fatalf("expected applications namespace %q, got %q", testApplicationsNamespace, mei2.ApplicationsNamespace)
	}
	if mei2.PerModuleImages[0].DeploymentName != testProvisioningDeploymentName {
		t.Fatalf("expected deployment name to be preserved, got %#v", mei2.PerModuleImages[0])
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
		status: &ModuleStatus{Conditions: []common.Condition{{
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

	if err := ComputeModulesStatusDetailed(context.Background(), rr); err != nil {
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

func TestComputeModulesStatusPreservesWorkbenchNamespaceOnGetModuleStatusError(t *testing.T) {
	withTestRegistry(t)

	const existingNamespace = "rhods-notebooks"
	handler := legacyStatusFieldsWriterStub{
		provisioningModuleStub: provisioningModuleStub{
			moduleName: "workbenches",
			enabled:    true,
		},
		getStatusErr: errors.New("failed to get module status"),
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))

	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsc.Spec.Components.Workbenches.WorkbenchNamespace = "other-namespace"
	dsc.Status.Components.Workbenches.WorkbenchesCommonStatus = &componentApi.WorkbenchesCommonStatus{
		WorkbenchNamespace: existingNamespace,
	}
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

	if err := ComputeModulesStatusDetailed(context.Background(), rr); err != nil {
		t.Fatalf("compute modules status: %v", err)
	}

	if got := dsc.Status.Components.Workbenches.WorkbenchNamespace; got != existingNamespace {
		t.Fatalf("expected workbench namespace %q to be preserved when module status read fails, got %q", existingNamespace, got)
	}
}

// Integration-level guard (ComputeModulesStatus over a fake client) for the
// reported symptom: a module that is Ready=True but reports an
// optional-dependency submodule condition as Info-severity False must NOT drag
// the DSC to Not Ready. The Info condition is surfaced on the DSC (visibility)
// but does not gate ModulesReady.
func TestComputeModulesStatusInfoDependencyKeepsModulesReady(t *testing.T) {
	withTestRegistry(t)

	const depCond = "KserveLLMInferenceServiceDependencies"

	handler := provisioningModuleStub{
		moduleName: "kserve",
		enabled:    true,
		status: &ModuleStatus{Conditions: []common.Condition{
			{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: "AllGood"},
			{
				Type:     depCond,
				Status:   metav1.ConditionFalse,
				Reason:   "PreConditionFailed",
				Message:  "Red Hat Connectivity Link not installed; cert-manager operator not installed",
				Severity: common.ConditionSeverityInfo,
			},
		}},
		submodules: []SubmoduleCondition{
			{SourceConditionType: depCond, DSCConditionType: depCond},
		},
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))

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

	if err := ComputeModulesStatusDetailed(context.Background(), rr); err != nil {
		t.Fatalf("compute modules status: %v", err)
	}

	// The dependency condition is surfaced on the DSC, with severity preserved.
	dep := conditions.FindStatusCondition(dsc.GetStatus(), depCond)
	if dep == nil {
		t.Fatalf("expected %s condition to be surfaced on the DSC", depCond)
	}
	if dep.Status != metav1.ConditionFalse {
		t.Fatalf("expected %s=False, got %s", depCond, dep.Status)
	}
	if dep.Severity != common.ConditionSeverityInfo {
		t.Fatalf("expected %s severity=Info to be preserved, got %q", depCond, dep.Severity)
	}

	// ...but it must not gate readiness: ModulesReady stays True.
	ready := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if ready == nil {
		t.Fatalf("expected ModulesReady condition to be set")
	}
	if ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected ModulesReady=True (Info dep is non-gating), got %s: %q", ready.Status, ready.Message)
	}
}

// noMatchErrorModuleStub is a ModuleHandler whose GetModuleStatus returns a
// NoKindMatchError, simulating a missing CRD.
type noMatchErrorModuleStub struct {
	provisioningModuleStub
}

func (s noMatchErrorModuleStub) GetModuleStatus(_ context.Context, _ client.Client) (*ModuleStatus, error) {
	return nil, &meta.NoKindMatchError{
		GroupKind: schema.GroupKind{
			Group: testProvisioningModuleGroup,
			Kind:  testProvisioningModuleKind,
		},
	}
}

func TestComputeModulesStatusRequeuesOnCRDAbsent(t *testing.T) {
	withTestRegistry(t)

	handler := noMatchErrorModuleStub{
		provisioningModuleStub: provisioningModuleStub{
			moduleName: "crd-absent-module",
			enabled:    true,
		},
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))

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

	err = ComputeModulesStatusDetailed(context.Background(), rr)

	// Must return a RequeueAfterError so the controller retries.
	var requeueErr odherrors.RequeueAfterError
	if !errors.As(err, &requeueErr) {
		t.Fatalf("expected RequeueAfterError when module CRD is absent, got: %v", err)
	}
	if requeueErr.After != 30*time.Second {
		t.Fatalf("expected 30s requeue delay, got %v", requeueErr.After)
	}

	// ModulesReady condition must still be set (aggregation ran despite the requeue).
	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if modulesReady == nil {
		t.Fatalf("expected ModulesReady condition to be set even when CRD is absent")
	}
	if modulesReady.Status != metav1.ConditionFalse {
		t.Fatalf("expected ModulesReady=False, got %s", modulesReady.Status)
	}

	// Per-module condition must mention the missing CRD.
	moduleCond := conditions.FindStatusCondition(dsc.GetStatus(), testProvisioningModuleKind+status.ReadySuffix)
	if moduleCond == nil {
		t.Fatalf("expected per-module condition to be set")
	}
	if moduleCond.Status != metav1.ConditionFalse {
		t.Fatalf("expected per-module condition=False, got %s", moduleCond.Status)
	}
}

func TestComputeModulesStatusNoRequeueOnRegularError(t *testing.T) {
	withTestRegistry(t)

	handler := legacyStatusFieldsWriterStub{
		provisioningModuleStub: provisioningModuleStub{
			moduleName: "error-module",
			enabled:    true,
		},
		getStatusErr: errors.New("some transient API error"),
	}
	DefaultRegistry().Add(handler, WithRunlevel(dag.RL(20)))

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

	err = ComputeModulesStatusDetailed(context.Background(), rr)

	// Regular errors should NOT trigger a requeue.
	if err != nil {
		t.Fatalf("expected nil error for regular GetModuleStatus failures, got: %v", err)
	}
}

func TestBuildPlatformContext_MonitoringNamespaceFromDSCI(t *testing.T) {
	const testMonitoringNS = "redhat-ods-monitoring"

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: testDSCIName},
	}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace
	dsci.Spec.Monitoring.Namespace = testMonitoringNS

	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:   cli,
		Instance: &configv1alpha1.Platform{},
	}

	ctx, err := buildPlatformContext(context.Background(), rr)
	if err != nil {
		t.Fatalf("buildPlatformContext: %v", err)
	}

	if ctx.ApplicationsNamespace != testApplicationsNamespace {
		t.Fatalf("expected applications namespace %q, got %q", testApplicationsNamespace, ctx.ApplicationsNamespace)
	}
	if ctx.MonitoringNamespace != testMonitoringNS {
		t.Fatalf("expected monitoring namespace %q, got %q", testMonitoringNS, ctx.MonitoringNamespace)
	}
}

func TestBuildPlatformContext_MonitoringNamespaceEmptyWithoutDSCI(t *testing.T) {
	cli, err := fakeclient.New()
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:   cli,
		Instance: &configv1alpha1.Platform{},
	}

	ctx, err := buildPlatformContext(context.Background(), rr)
	if err == nil {
		if ctx.MonitoringNamespace != "" {
			t.Fatalf("expected empty monitoring namespace without DSCI, got %q", ctx.MonitoringNamespace)
		}
	}
	// ApplicationNamespace also fails without DSCI — that's expected.
	// The key assertion: no panic, monitoring namespace is empty.
}

func TestProvisionModulesMonitoringNamespaceInjected(t *testing.T) {
	withTestRegistry(t)

	const testMonitoringNS = "redhat-ods-monitoring"

	handler := provisioningModuleStub{
		moduleName: testProvisioningModuleName,
		enabled:    true,
		status: &ModuleStatus{
			Conditions: []common.Condition{{
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
	dsci.Spec.Monitoring.Namespace = testMonitoringNS

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

	mei := types.GetModuleEnvInjection(rr)
	if mei == nil {
		t.Fatalf("expected module env injection to be set")
	}
	if mei.MonitoringNamespace != testMonitoringNS {
		t.Fatalf("expected monitoring namespace %q, got %q", testMonitoringNS, mei.MonitoringNamespace)
	}
}

func TestBuildPlatformModules_NoEmptyManagementState(t *testing.T) {
	t.Parallel()

	dsc := &dscv2.DataScienceCluster{}
	pm := BuildPlatformModules(&DSCContext{DSC: dsc})

	v := reflect.ValueOf(pm)
	for sf, fv := range v.Fields() {
		ms := fv.FieldByName("ManagementState")
		if !ms.IsValid() {
			continue
		}
		state := operatorv1.ManagementState(ms.String())
		if state == "" {
			t.Errorf("PlatformModules.%s.ManagementState is empty; must be Managed or Removed", sf.Name)
		}
		if state != operatorv1.Managed && state != operatorv1.Removed {
			t.Errorf("PlatformModules.%s.ManagementState = %q; want Managed or Removed", sf.Name, state)
		}
	}
}

func TestBuildPlatformModules_WithDSCI_MonitoringManaged(t *testing.T) {
	withTestRegistry(t)

	DefaultRegistry().Add(&dsciMonitoringHandler{
		BaseHandler: BaseHandler{Config: ModuleConfig{Name: "monitoring"}},
	}, WithConfigSource(ConfigFromDSCI))

	dsci := &dsciv2.DSCInitialization{
		Spec: dsciv2.DSCInitializationSpec{
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
			},
		},
	}

	pm := BuildPlatformModules(&DSCContext{
		DSC:  &dscv2.DataScienceCluster{},
		DSCI: dsci,
	})

	if pm.Monitoring.ManagementState != operatorv1.Managed {
		t.Fatalf("expected monitoring=Managed when DSCI is provided, got %q", pm.Monitoring.ManagementState)
	}
}

func TestBuildPlatformModulesForSource_DoesNotPopulateOtherSource(t *testing.T) {
	withTestRegistry(t)

	DefaultRegistry().Add(&dsciMonitoringHandler{
		BaseHandler: BaseHandler{Config: ModuleConfig{Name: "monitoring"}},
	}, WithConfigSource(ConfigFromDSCI))
	DefaultRegistry().Add(&dscDashboardHandler{
		BaseHandler: BaseHandler{Config: ModuleConfig{Name: "dashboard"}},
	})

	dsc := &dscv2.DataScienceCluster{}
	dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
	dsci := &dsciv2.DSCInitialization{
		Spec: dsciv2.DSCInitializationSpec{
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
			},
		},
	}
	dscCtx := &DSCContext{DSC: dsc, DSCI: dsci}

	dsciModules := BuildPlatformModulesForSource(dscCtx, ConfigFromDSCI)
	if dsciModules.Monitoring.ManagementState != operatorv1.Managed {
		t.Fatalf("expected DSCI source to set monitoring=Managed, got %q", dsciModules.Monitoring.ManagementState)
	}
	if dsciModules.Dashboard.ManagementState != "" {
		t.Fatalf("expected DSCI source to leave dashboard unset, got %q", dsciModules.Dashboard.ManagementState)
	}

	dscModules := BuildPlatformModulesForSource(dscCtx, ConfigFromDSC)
	if dscModules.Dashboard.ManagementState != operatorv1.Managed {
		t.Fatalf("expected DSC source to set dashboard=Managed, got %q", dscModules.Dashboard.ManagementState)
	}
	if dscModules.Monitoring.ManagementState != "" {
		t.Fatalf("expected DSC source to leave monitoring unset, got %q", dscModules.Monitoring.ManagementState)
	}
}

type dsciMonitoringHandler struct {
	BaseHandler
}

func (h *dsciMonitoringHandler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Monitoring.ManagementState == operatorv1.Managed
}

func (h *dsciMonitoringHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (h *dsciMonitoringHandler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSCI == nil {
		return
	}
	ms := dscCtx.DSCI.Spec.Monitoring.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Monitoring.ManagementState = ms
}

type dscDashboardHandler struct {
	BaseHandler
}

func (h *dscDashboardHandler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Dashboard.ManagementState == operatorv1.Managed
}

func (h *dscDashboardHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (h *dscDashboardHandler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.Dashboard.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Dashboard.ManagementState = ms
}
