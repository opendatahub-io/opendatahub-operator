//nolint:testpackage // Exercises package-private module lifecycle wiring directly.
package modules

import (
	"context"
	"testing"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

const (
	lifecycleTestVersion = "2.30.0"
	lifecycleTestDSC     = "lifecycle-dsc"
	lifecycleTestDSCI    = "lifecycle-dsci"
	lifecycleTestAppNS   = "opendatahub"
)

// lifecycleModuleStub is a configurable stub that tracks calls.
type lifecycleModuleStub struct {
	name         string
	gvk          schema.GroupVersionKind
	enabled      bool
	crState      CRState
	moduleStatus *ModuleStatus

	deleteCRCalls       int
	deleteOperatorCalls int
}

func (s *lifecycleModuleStub) GetName() string                                { return s.name }
func (s *lifecycleModuleStub) IsEnabled(*configv1alpha1.PlatformModules) bool { return s.enabled }
func (s *lifecycleModuleStub) GetGVK() schema.GroupVersionKind                { return s.gvk }

func (s *lifecycleModuleStub) PopulatePlatformModule(_ *configv1alpha1.PlatformModules, _ *DSCContext) {
}

func (s *lifecycleModuleStub) GetOperatorManifests(*PlatformContext) OperatorManifests {
	return OperatorManifests{
		Manifests: []types.ManifestInfo{{Path: s.name, SourcePath: "overlays/odh"}},
	}
}

func (s *lifecycleModuleStub) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(s.gvk)
	u.SetName("default-" + s.name)
	return u, nil
}

func (s *lifecycleModuleStub) GetRelatedImages() []string { return nil }

func (s *lifecycleModuleStub) GetModuleStatus(_ context.Context, _ client.Client) (*ModuleStatus, error) {
	if s.moduleStatus != nil {
		return s.moduleStatus, nil
	}
	return &ModuleStatus{}, nil
}

func (s *lifecycleModuleStub) GetModuleCRState(_ context.Context, _ client.Client) (CRState, error) {
	return s.crState, nil
}

func (s *lifecycleModuleStub) DeleteModuleCR(_ context.Context, _ client.Client) error {
	s.deleteCRCalls++
	return nil
}

func (s *lifecycleModuleStub) DeleteOperatorResources(_ context.Context, _ client.Client, _ *PlatformContext) error {
	s.deleteOperatorCalls++
	return nil
}

func (s *lifecycleModuleStub) WriteDSCComponentStatus(*dscv2.DataScienceCluster, bool, []common.ComponentRelease) {
}

func (s *lifecycleModuleStub) GetDeploymentName() string { return s.name + "-controller-manager" }

func lifecycleRR(t *testing.T) (*types.ReconciliationRequest, *dscv2.DataScienceCluster) {
	t.Helper()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: lifecycleTestDSC, UID: "uid-lifecycle"},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: lifecycleTestDSCI},
	}
	dsci.Spec.ApplicationsNamespace = lifecycleTestAppNS

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	return &types.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
		Release: common.Release{
			Name:    common.Platform("Open Data Hub"),
			Version: ofversion.OperatorVersion{Version: semver.MustParse(lifecycleTestVersion)},
		},
		Conditions: conditions.NewManager(dsc, status.ConditionTypeModulesReady),
	}, dsc
}

// TestLifecycle_ProvisionThenDisableThenCleanup verifies the full module
// lifecycle state machine:
//  1. Enabled module → provisionModules adds operator manifests
//  2. Module disabled, CR still alive → cleanupDisabledModules keeps operator
//     manifests alive (CR deletion is owned by DSC/DSCI controllers)
//  3. Module disabled, CR absent → cleanupDisabledModules removes operator resources
func TestLifecycle_ProvisionThenDisableThenCleanup(t *testing.T) {
	withTestRegistry(t)

	moduleA := &lifecycleModuleStub{
		name:    "module-a",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "ModuleA"},
		enabled: true,
		crState: CRStateAlive,
		moduleStatus: &ModuleStatus{
			Conditions: []common.Condition{{
				Type:   status.ConditionTypeReady,
				Status: metav1.ConditionTrue,
			}},
		},
	}
	moduleB := &lifecycleModuleStub{
		name:    "module-b",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "ModuleB"},
		enabled: true,
		crState: CRStateAlive,
		moduleStatus: &ModuleStatus{
			Conditions: []common.Condition{{
				Type:   status.ConditionTypeReady,
				Status: metav1.ConditionTrue,
			}},
		},
	}

	DefaultRegistry().Add(moduleA, WithRunlevel(dag.RL(20)))
	DefaultRegistry().Add(moduleB, WithRunlevel(dag.RL(30)))
	provision.Add(moduleA.name, provision.KindModule, dag.RL(20))
	provision.Add(moduleB.name, provision.KindModule, dag.RL(30))

	// Phase 1: Both enabled — provision should add manifests for both.
	rr, dsc := lifecycleRR(t)
	if err := provisionModules(context.Background(), rr); err != nil {
		t.Fatalf("phase 1 provision: %v", err)
	}
	if len(rr.Manifests) != 2 {
		t.Fatalf("phase 1: expected 2 manifest entries, got %d", len(rr.Manifests))
	}

	// Phase 2: Disable module-b, CR is still alive → cleanup keeps operator
	// manifests alive (CR deletion is handled by DSC/DSCI controllers, not
	// the platform controller's cleanup action).
	moduleB.enabled = false
	moduleB.crState = CRStateAlive

	rr2, _ := lifecycleRR(t)
	if err := cleanupDisabledModules(context.Background(), rr2); err != nil {
		t.Fatalf("phase 2 cleanup: %v", err)
	}
	if moduleB.deleteCRCalls != 0 {
		t.Fatalf("phase 2: cleanup should NOT delete module CR (owned by DSC controller), got %d calls", moduleB.deleteCRCalls)
	}
	if moduleB.deleteOperatorCalls != 0 {
		t.Fatalf("phase 2: should NOT delete operator resources while CR is still alive")
	}
	if len(rr2.Manifests) != 1 {
		t.Fatalf("phase 2: expected 1 manifest entry (operator kept alive), got %d", len(rr2.Manifests))
	}
	if moduleA.deleteCRCalls != 0 {
		t.Fatalf("phase 2: module-a (still enabled) should not be cleaned up")
	}

	// Phase 3: module-b CR is now absent → cleanup removes operator resources.
	moduleB.crState = CRStateAbsent

	rr3, _ := lifecycleRR(t)
	if err := cleanupDisabledModules(context.Background(), rr3); err != nil {
		t.Fatalf("phase 3 cleanup: %v", err)
	}
	if moduleB.deleteOperatorCalls != 1 {
		t.Fatalf("phase 3: expected 1 DeleteOperatorResources call, got %d", moduleB.deleteOperatorCalls)
	}

	// Verify module-a is still provisionable after module-b's full cleanup.
	rr4, _ := lifecycleRR(t)
	if err := provisionModules(context.Background(), rr4); err != nil {
		t.Fatalf("phase 4 re-provision: %v", err)
	}
	if len(rr4.Manifests) != 1 {
		t.Fatalf("phase 4: expected 1 manifest entry (only module-a), got %d", len(rr4.Manifests))
	}

	// Verify status computation reflects the final state.
	rr5, _ := lifecycleRR(t)
	rr5.Instance = dsc
	rr5.Conditions = conditions.NewManager(dsc, status.ConditionTypeModulesReady)
	if err := ComputeModulesStatusDetailed(context.Background(), rr5); err != nil {
		t.Fatalf("status computation: %v", err)
	}
	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if modulesReady == nil {
		t.Fatalf("expected ModulesReady condition")
	}
	if modulesReady.Status != metav1.ConditionTrue {
		t.Fatalf("expected ModulesReady=True (module-a is ready, module-b is disabled), got %s: %s",
			modulesReady.Status, modulesReady.Message)
	}
}

// TestLifecycle_CleanupWithDeletingCR verifies the CRStateDeleting → CRStateAbsent
// transition preserves operator manifests during Phase 1.
func TestLifecycle_CleanupWithDeletingCR(t *testing.T) {
	withTestRegistry(t)

	mod := &lifecycleModuleStub{
		name:    "deleting-module",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "DeletingModule"},
		enabled: false,
		crState: CRStateDeleting,
	}

	DefaultRegistry().Add(mod, WithRunlevel(dag.RL(20)))
	provision.Add(mod.name, provision.KindModule, dag.RL(20))

	rr, _ := lifecycleRR(t)
	if err := cleanupDisabledModules(context.Background(), rr); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if mod.deleteCRCalls != 0 {
		t.Fatalf("should not call DeleteModuleCR when CR is already deleting")
	}
	if mod.deleteOperatorCalls != 0 {
		t.Fatalf("should not delete operator resources while CR is deleting")
	}
	if len(rr.Manifests) != 1 {
		t.Fatalf("expected operator manifests to be preserved, got %d", len(rr.Manifests))
	}
}

// TestLifecycle_MultiModuleStatusAggregation verifies that ComputeModulesStatus
// correctly aggregates conditions from multiple modules with different states.
func TestLifecycle_MultiModuleStatusAggregation(t *testing.T) {
	withTestRegistry(t)

	readyModule := &lifecycleModuleStub{
		name:    "ready-mod",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "ReadyMod"},
		enabled: true,
		crState: CRStateAlive,
		moduleStatus: &ModuleStatus{
			Conditions: []common.Condition{{
				Type:   status.ConditionTypeReady,
				Status: metav1.ConditionTrue,
				Reason: "AllGood",
			}},
		},
	}
	degradedModule := &lifecycleModuleStub{
		name:    "degraded-mod",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "DegradedMod"},
		enabled: true,
		crState: CRStateAlive,
		moduleStatus: &ModuleStatus{
			Conditions: []common.Condition{
				{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: "Ready"},
				{Type: status.ConditionTypeDegraded, Status: metav1.ConditionTrue, Reason: "PartialFailure"},
			},
		},
	}
	disabledModule := &lifecycleModuleStub{
		name:    "disabled-mod",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "DisabledMod"},
		enabled: false,
		crState: CRStateAbsent,
	}

	DefaultRegistry().Add(readyModule, WithRunlevel(dag.RL(20)))
	DefaultRegistry().Add(degradedModule, WithRunlevel(dag.RL(20)))
	DefaultRegistry().Add(disabledModule, WithRunlevel(dag.RL(20)))

	rr, dsc := lifecycleRR(t)
	rr.Conditions = conditions.NewManager(dsc, status.ConditionTypeModulesReady)

	if err := ComputeModulesStatusDetailed(context.Background(), rr); err != nil {
		t.Fatalf("compute status: %v", err)
	}

	readyModCond := conditions.FindStatusCondition(dsc.GetStatus(), "ReadyModReady")
	if readyModCond == nil || readyModCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected ReadyModReady=True, got %v", readyModCond)
	}

	degradedModCond := conditions.FindStatusCondition(dsc.GetStatus(), "DegradedModReady")
	if degradedModCond == nil || degradedModCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected DegradedModReady=True (Ready is True even if Degraded), got %v", degradedModCond)
	}

	disabledModCond := conditions.FindStatusCondition(dsc.GetStatus(), "DisabledModReady")
	if disabledModCond == nil || disabledModCond.Status != metav1.ConditionFalse || disabledModCond.Reason != status.RemovedReason {
		t.Fatalf("expected DisabledModReady=False/Removed, got %v", disabledModCond)
	}

	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if modulesReady == nil {
		t.Fatalf("expected ModulesReady condition")
	}
	if modulesReady.Status != metav1.ConditionFalse || modulesReady.Reason != status.ConditionTypeDegraded {
		t.Fatalf("expected ModulesReady=False/Degraded (one module is degraded), got status=%s reason=%s",
			modulesReady.Status, modulesReady.Reason)
	}
}

// TestLifecycle_AllDisabledReportsNoManagedModules verifies that when every
// module is disabled, ModulesReady is True with reason NoManagedModules.
func TestLifecycle_AllDisabledReportsNoManagedModules(t *testing.T) {
	withTestRegistry(t)

	mod := &lifecycleModuleStub{
		name:    "only-module",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "OnlyModule"},
		enabled: false,
		crState: CRStateAbsent,
	}

	DefaultRegistry().Add(mod, WithRunlevel(dag.RL(20)))

	rr, dsc := lifecycleRR(t)
	rr.Conditions = conditions.NewManager(dsc, status.ConditionTypeModulesReady)

	if err := ComputeModulesStatusDetailed(context.Background(), rr); err != nil {
		t.Fatalf("compute status: %v", err)
	}

	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if modulesReady == nil {
		t.Fatalf("expected ModulesReady condition")
	}
	if modulesReady.Status != metav1.ConditionTrue || modulesReady.Reason != status.NoManagedModulesReason {
		t.Fatalf("expected ModulesReady=True/%s, got status=%s reason=%s",
			status.NoManagedModulesReason, modulesReady.Status, modulesReady.Reason)
	}
}

// TestLifecycle_StaleModuleStatusMarksNotReady verifies that a module whose
// observedGeneration is behind its generation is reported as not ready.
func TestLifecycle_StaleModuleStatusMarksNotReady(t *testing.T) {
	withTestRegistry(t)

	mod := &lifecycleModuleStub{
		name:    "stale-module",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "StaleModule"},
		enabled: true,
		crState: CRStateAlive,
		moduleStatus: &ModuleStatus{
			Conditions: []common.Condition{{
				Type:   status.ConditionTypeReady,
				Status: metav1.ConditionTrue,
			}},
			ObservedGeneration: 1,
			Generation:         3,
		},
	}

	DefaultRegistry().Add(mod, WithRunlevel(dag.RL(20)))

	rr, dsc := lifecycleRR(t)
	rr.Conditions = conditions.NewManager(dsc, status.ConditionTypeModulesReady)

	if err := ComputeModulesStatusDetailed(context.Background(), rr); err != nil {
		t.Fatalf("compute status: %v", err)
	}

	moduleCond := conditions.FindStatusCondition(dsc.GetStatus(), "StaleModuleReady")
	if moduleCond == nil || moduleCond.Status != metav1.ConditionFalse {
		t.Fatalf("expected StaleModuleReady=False (stale generation), got %v", moduleCond)
	}

	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	if modulesReady == nil || modulesReady.Status != metav1.ConditionFalse {
		t.Fatalf("expected ModulesReady=False when a module has stale status")
	}
}
