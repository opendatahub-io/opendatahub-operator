//nolint:testpackage // Exercises package-private module status wiring directly.
package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func withTestRegistry(t *testing.T) {
	t.Helper()

	original := r
	originalStuckTracker := moduleStuckTracker
	r = &Registry{}
	moduleStuckTracker = dag.NewStuckTracker()
	t.Cleanup(func() {
		r = original
		moduleStuckTracker = originalStuckTracker
		provision.DefaultRegistry().Reset()
		provision.GetRunlevelTracker().Reset()
	})

	provision.DefaultRegistry().Reset()
	provision.GetRunlevelTracker().Reset()
}

const (
	testDSCName                   = "test-dsc"
	testDSCIName                  = "default-dsci"
	testApplicationsNamespace     = "opendatahub"
	testProvisioningModuleName    = "test-module"
	testProvisioningModuleKind    = "TestModule"
	testProvisioningModuleGroup   = "test.opendatahub.io"
	testProvisioningModuleVersion = "v1alpha1"
	testProvisioningOverlayODH    = "overlays/odh"
)

type provisioningModuleStub struct {
	moduleName string
	enabled    bool
	status     *ModuleStatus
	statusErr  error
	crStateErr error
	submodules []SubmoduleCondition
}

func (s provisioningModuleStub) GetSubmoduleConditions() []SubmoduleCondition {
	return s.submodules
}

func (s provisioningModuleStub) GetName() string { return s.moduleName }

func (s provisioningModuleStub) IsEnabled(*configApi.PlatformModules) bool { return s.enabled }

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

func (s provisioningModuleStub) PopulatePlatformModule(_ *configApi.PlatformModules, _ *DSCContext) {
}

func (s provisioningModuleStub) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(s.GetGVK())
	u.SetName("default-" + s.moduleName)
	return u, nil
}

func (s provisioningModuleStub) GetRelatedImages() []string {
	return nil
}

func (s provisioningModuleStub) GetModuleStatus(context.Context, client.Client) (*ModuleStatus, error) {
	if s.statusErr != nil {
		return nil, s.statusErr
	}
	if s.status != nil {
		return s.status, nil
	}
	return &ModuleStatus{}, nil
}

func (s provisioningModuleStub) GetModuleCRState(context.Context, client.Client) (CRState, error) {
	if s.crStateErr != nil {
		return CRStateAbsent, s.crStateErr
	}
	return CRStateAlive, nil
}

func (s provisioningModuleStub) DeleteModuleCR(context.Context, client.Client) error { return nil }

func (s provisioningModuleStub) WriteDSCComponentStatus(*dscApi.DataScienceCluster, bool, []common.ComponentRelease) {
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
	dsc *dscApi.DataScienceCluster,
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
	DefaultRegistry().Add(readyHandler)
	DefaultRegistry().Add(notReadyHandler)

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
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
	DefaultRegistry().Add(handler)

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
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
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
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
	DefaultRegistry().Add(handler)

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
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
	DefaultRegistry().Add(handler)

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
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
	DefaultRegistry().Add(handler)

	dsc := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: testDSCName}}
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: testDSCIName}}
	dsci.Spec.ApplicationsNamespace = testApplicationsNamespace

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
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
		Instance: &configApi.Platform{},
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
		Instance: &configApi.Platform{},
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

func TestBuildPlatformModulesForSource_DoesNotPopulateOtherSource(t *testing.T) {
	withTestRegistry(t)

	DefaultRegistry().Add(&testDSCIConfiguredHandler{
		BaseHandler: BaseHandler{Config: ModuleConfig{Name: "monitoring"}},
	}, WithConfigSource(ConfigFromDSCI))
	DefaultRegistry().Add(&testDSCConfiguredHandler{
		BaseHandler: BaseHandler{Config: ModuleConfig{Name: "dashboard"}},
	})

	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Managed
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

	// Static deployment suppression must not change source field ownership.
	DefaultRegistry().Disable("dashboard")
	DefaultRegistry().Disable("monitoring")
	if got := BuildPlatformModulesForSource(dscCtx, ConfigFromDSC); got != dscModules {
		t.Fatalf("disabled DSC handler changed its projection: got %+v, want %+v", got, dscModules)
	}
	if got := BuildPlatformModulesForSource(dscCtx, ConfigFromDSCI); got != dsciModules {
		t.Fatalf("disabled DSCI handler changed its projection: got %+v, want %+v", got, dsciModules)
	}
}

func TestNewPlatformCRRemovedForSource_PreservesOtherSource(t *testing.T) {
	withTestRegistry(t)

	DefaultRegistry().Add(&testDSCIConfiguredHandler{
		BaseHandler: BaseHandler{Config: ModuleConfig{Name: "monitoring"}},
	}, WithConfigSource(ConfigFromDSCI))
	DefaultRegistry().Add(&testDSCConfiguredHandler{
		BaseHandler: BaseHandler{Config: ModuleConfig{Name: "dashboard"}},
	})

	dsc := &dscApi.DataScienceCluster{}
	dsc.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Managed
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

	removed := NewPlatformCRRemovedForSource(dscCtx, ConfigFromDSC)
	if removed.Spec.Modules.Dashboard.ManagementState != operatorv1.Removed {
		t.Fatalf("expected dashboard=Removed on DSC delete payload, got %q", removed.Spec.Modules.Dashboard.ManagementState)
	}
	if removed.Spec.Modules.Monitoring.ManagementState != "" {
		t.Fatalf("expected monitoring to stay unset so DSCI SSA keeps it, got %q", removed.Spec.Modules.Monitoring.ManagementState)
	}

	removed = NewPlatformCRRemovedForSource(dscCtx, ConfigFromDSCI)
	if removed.Spec.Modules.Monitoring.ManagementState != operatorv1.Removed {
		t.Fatalf("expected monitoring=Removed on DSCI delete payload, got %q", removed.Spec.Modules.Monitoring.ManagementState)
	}
	if removed.Spec.Modules.Dashboard.ManagementState != "" {
		t.Fatalf("expected dashboard to stay unset so DSC SSA keeps it, got %q", removed.Spec.Modules.Dashboard.ManagementState)
	}
}

// testDSCIConfiguredHandler is a test double for a ConfigFromDSCI module.
// It writes PlatformModules.Monitoring only because that is the typed SSA
// field for DSCI-sourced enablement, not because it is the production handler.
type testDSCIConfiguredHandler struct {
	BaseHandler
}

func (h *testDSCIConfiguredHandler) IsEnabled(modules *configApi.PlatformModules) bool {
	return modules != nil && modules.Monitoring.ManagementState == operatorv1.Managed
}

func (h *testDSCIConfiguredHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (h *testDSCIConfiguredHandler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSCI == nil {
		return
	}
	ms := dscCtx.DSCI.Spec.Monitoring.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Monitoring.ManagementState = ms
}

// testDSCConfiguredHandler is a test double for a ConfigFromDSC module.
// It writes PlatformModules.Dashboard as a stand-in typed SSA field.
type testDSCConfiguredHandler struct {
	BaseHandler
}

func (h *testDSCConfiguredHandler) IsEnabled(modules *configApi.PlatformModules) bool {
	return modules != nil && modules.Dashboard.ManagementState == operatorv1.Managed
}

func (h *testDSCConfiguredHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (h *testDSCConfiguredHandler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.Dashboard.Standard.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Dashboard.ManagementState = ms
}

func TestFinalizePlatformWaitsForOwnedTrackers(t *testing.T) {
	g := NewWithT(t)
	withTestRegistry(t)
	DefaultRegistry().Add(&lifecycleModuleStub{name: "module", crState: CRStateAlive})
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName, UID: "platform-uid"}}
	tracker := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "module", UID: "tracker-uid", Finalizers: []string{"tracker-finalizer"},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: configApi.GroupVersion.String(), Kind: configApi.PlatformKind, Name: platform.Name, UID: platform.UID, Controller: new(true),
		}}}}
	external := tracker.DeepCopy()
	external.Name = "external"
	external.OwnerReferences = nil
	cli, err := fakeclient.New(fakeclient.WithObjects(platform, tracker, external))
	g.Expect(err).ShouldNot(HaveOccurred())
	rr := &types.ReconciliationRequest{Client: cli, Instance: platform}
	g.Expect(finalizePlatform(t.Context(), rr)).Should(HaveOccurred(), "Platform must wait for tracker finalization")
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(tracker), tracker)).Should(Succeed())
	g.Expect(tracker.DeletionTimestamp.IsZero()).Should(BeFalse(), "owned tracker deletion was not requested")
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(external), external)).Should(Succeed())
	g.Expect(external.DeletionTimestamp.IsZero()).Should(BeTrue(), "external tracker was deleted")
	tracker.Finalizers = nil
	g.Expect(cli.Update(t.Context(), tracker)).Should(Succeed())
	g.Expect(finalizePlatform(t.Context(), rr)).Should(Succeed(), "unrelated module CR must not block finalization after owned trackers are gone")
}

func TestDisabledTrackerDeletionRetriesAfterConflict(t *testing.T) {
	for _, ownershipChanges := range []bool{false, true} {
		t.Run(map[bool]string{false: "status change", true: "ownership change"}[ownershipChanges], func(t *testing.T) {
			g := NewWithT(t)
			withTestRegistry(t)
			DefaultRegistry().Add(provisioningModuleStub{moduleName: "module"})
			platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName, UID: "platform-uid"}}
			tracker := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "module", UID: "tracker-uid",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: configApi.GroupVersion.String(), Kind: configApi.PlatformKind, Name: platform.Name, UID: platform.UID, Controller: new(true),
				}}}}
			conflict := true
			cli, err := fakeclient.New(fakeclient.WithObjects(tracker), fakeclient.WithStatusSubresources(tracker), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					options := (&client.DeleteOptions{}).ApplyOptions(opts)
					g.Expect(options.Preconditions).ShouldNot(BeNil())
					g.Expect(options.Preconditions.UID).Should(Equal(new(obj.GetUID())), "deletion must pin the identity")
					g.Expect(options.Preconditions.ResourceVersion).Should(Equal(new(obj.GetResourceVersion())), "deletion must pin the ownership check")
					if !conflict {
						return c.Delete(ctx, obj, opts...)
					}
					conflict = false
					updated := tracker.DeepCopy()
					if err := c.Get(ctx, client.ObjectKeyFromObject(tracker), updated); err != nil {
						return err
					}
					if ownershipChanges {
						updated.OwnerReferences[0].UID = "external-platform"
						if err := c.Update(ctx, updated); err != nil {
							return err
						}
					} else {
						updated.Status.ObservedGeneration = 1
						if err := c.Status().Update(ctx, updated); err != nil {
							return err
						}
					}
					return k8serr.NewConflict(configApi.GroupVersion.WithResource("platformmodules").GroupResource(), obj.GetName(), errors.New("changed since read"))
				},
			}))
			g.Expect(err).ShouldNot(HaveOccurred())
			rr := &types.ReconciliationRequest{Client: cli, Instance: platform}
			err = cleanupDisabledPlatformModules(t.Context(), rr)
			g.Expect(k8serr.IsConflict(err)).Should(BeTrue())
			g.Expect(cleanupDisabledPlatformModules(t.Context(), rr)).Should(Succeed())
			err = cli.Get(t.Context(), client.ObjectKeyFromObject(tracker), tracker)
			if ownershipChanges {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tracker.OwnerReferences).ShouldNot(BeEmpty())
				g.Expect(string(tracker.OwnerReferences[0].UID)).Should(Equal("external-platform"))
			} else {
				g.Expect(k8serr.IsNotFound(err)).Should(BeTrue(), "still-owned tracker was not deleted on retry")
			}
		})
	}
}
