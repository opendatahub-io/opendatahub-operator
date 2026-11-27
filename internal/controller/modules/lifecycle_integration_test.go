//nolint:testpackage // Exercises package-private module lifecycle wiring directly.
package modules

import (
	"context"
	"testing"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
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
}

func (s *lifecycleModuleStub) GetName() string                           { return s.name }
func (s *lifecycleModuleStub) IsEnabled(*configApi.PlatformModules) bool { return s.enabled }
func (s *lifecycleModuleStub) GetGVK() schema.GroupVersionKind           { return s.gvk }

func (s *lifecycleModuleStub) PopulatePlatformModule(_ *configApi.PlatformModules, _ *DSCContext) {
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
	return nil
}

func (s *lifecycleModuleStub) WriteDSCComponentStatus(*dscApi.DataScienceCluster, bool, []common.ComponentRelease) {
}

func (s *lifecycleModuleStub) GetDeploymentName() string { return s.name + "-controller-manager" }

func lifecycleRR(t *testing.T) (*types.ReconciliationRequest, *dscApi.DataScienceCluster) {
	t.Helper()
	g := NewWithT(t)

	dsc := &dscApi.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: lifecycleTestDSC, UID: "uid-lifecycle"},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: lifecycleTestDSCI},
	}
	dsci.Spec.ApplicationsNamespace = lifecycleTestAppNS

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	return &types.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
		Release: common.Release{
			Name:    cluster.OpenDataHub,
			Version: ofversion.OperatorVersion{Version: semver.MustParse(lifecycleTestVersion)},
		},
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeModulesReady),
	}, dsc
}

// TestLifecycle_ProvisionThenDisableThenCleanup verifies the tracker half of
// the module lifecycle. Operator deployment and two-phase operator cleanup are
// owned by the PlatformModule controller; the Platform controller creates the
// tracker while the module is enabled and deletes it when the module is disabled.
func TestLifecycle_ProvisionThenDisableThenCleanup(t *testing.T) {
	g := NewWithT(t)
	withTestRegistry(t)

	module := &lifecycleModuleStub{
		name:    "module-a",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "ModuleA"},
		enabled: true,
	}
	DefaultRegistry().Add(module)

	platform := &configApi.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName, UID: "platform-uid"},
	}
	// Tracker enablement and removal only need Platform.Spec.Modules.
	cli, err := fakeclient.New(fakeclient.WithObjects(platform))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:   cli,
		Instance: platform,
		Release: common.Release{
			Name:    cluster.OpenDataHub,
			Version: ofversion.OperatorVersion{Version: semver.MustParse(lifecycleTestVersion)},
		},
	}

	// Phase 1: the enabled module is represented by a PlatformModule tracker.
	g.Expect(provisionPlatformModules(context.Background(), rr)).Should(Succeed())
	g.Expect(rr.Resources).Should(HaveLen(1))
	g.Expect(rr.Resources[0].GetName()).Should(Equal(module.name))
	g.Expect(rr.Resources[0].GetAPIVersion()).Should(Equal(configApi.GroupVersion.String()))

	tracker := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: module.name, OwnerReferences: []metav1.OwnerReference{{
		APIVersion: configApi.GroupVersion.String(), Kind: configApi.PlatformKind, Name: platform.Name, UID: platform.UID, Controller: new(true),
	}}}}
	g.Expect(cli.Create(context.Background(), tracker)).Should(Succeed())

	// Phase 2: disabling the module deletes the tracker. Its finalizer then
	// owns the operator cleanup in the PlatformModule controller.
	module.enabled = false
	rr.Resources = nil
	g.Expect(cleanupDisabledPlatformModules(context.Background(), rr)).Should(Succeed())
	err = cli.Get(context.Background(), client.ObjectKey{Name: module.name}, &configApi.PlatformModule{})
	g.Expect(k8serr.IsNotFound(err)).Should(BeTrue())
}

// TestLifecycle_MultiModuleStatusAggregation verifies that ComputeModulesStatus
// correctly aggregates conditions from multiple modules with different states.
func TestLifecycle_MultiModuleStatusAggregation(t *testing.T) {
	g := NewWithT(t)
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

	DefaultRegistry().Add(readyModule)
	DefaultRegistry().Add(degradedModule)
	DefaultRegistry().Add(disabledModule)

	rr, dsc := lifecycleRR(t)
	rr.Conditions = conditionstest.NewManager(dsc, status.ConditionTypeModulesReady)

	g.Expect(ComputeModulesStatusDetailed(context.Background(), rr)).Should(Succeed())

	readyModCond := conditions.FindStatusCondition(dsc.GetStatus(), "ReadyModReady")
	g.Expect(readyModCond).ShouldNot(BeNil())
	g.Expect(readyModCond.Status).Should(Equal(metav1.ConditionTrue))

	degradedModCond := conditions.FindStatusCondition(dsc.GetStatus(), "DegradedModReady")
	g.Expect(degradedModCond).ShouldNot(BeNil())
	g.Expect(degradedModCond.Status).Should(Equal(metav1.ConditionTrue))

	disabledModCond := conditions.FindStatusCondition(dsc.GetStatus(), "DisabledModReady")
	g.Expect(disabledModCond).ShouldNot(BeNil())
	g.Expect(disabledModCond.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(disabledModCond.Reason).Should(Equal(status.RemovedReason))

	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	g.Expect(modulesReady).ShouldNot(BeNil())
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(modulesReady.Reason).Should(Equal(status.ConditionTypeDegraded))
}

// TestLifecycle_AllDisabledReportsNoManagedModules verifies that when every
// module is disabled, ModulesReady is True with reason NoManagedModules.
func TestLifecycle_AllDisabledReportsNoManagedModules(t *testing.T) {
	g := NewWithT(t)
	withTestRegistry(t)

	mod := &lifecycleModuleStub{
		name:    "only-module",
		gvk:     schema.GroupVersionKind{Group: "test.opendatahub.io", Version: "v1alpha1", Kind: "OnlyModule"},
		enabled: false,
		crState: CRStateAbsent,
	}

	DefaultRegistry().Add(mod)

	rr, dsc := lifecycleRR(t)
	rr.Conditions = conditionstest.NewManager(dsc, status.ConditionTypeModulesReady)

	g.Expect(ComputeModulesStatusDetailed(context.Background(), rr)).Should(Succeed())

	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	g.Expect(modulesReady).ShouldNot(BeNil())
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(modulesReady.Reason).Should(Equal(status.NoManagedModulesReason))
}

// TestLifecycle_StaleModuleStatusMarksNotReady verifies that a module whose
// observedGeneration is behind its generation is reported as not ready.
func TestLifecycle_StaleModuleStatusMarksNotReady(t *testing.T) {
	g := NewWithT(t)
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

	DefaultRegistry().Add(mod)

	rr, dsc := lifecycleRR(t)
	rr.Conditions = conditionstest.NewManager(dsc, status.ConditionTypeModulesReady)

	g.Expect(ComputeModulesStatusDetailed(context.Background(), rr)).Should(Succeed())

	moduleCond := conditions.FindStatusCondition(dsc.GetStatus(), "StaleModuleReady")
	g.Expect(moduleCond).ShouldNot(BeNil())
	g.Expect(moduleCond.Status).Should(Equal(metav1.ConditionFalse))

	modulesReady := conditions.FindStatusCondition(dsc.GetStatus(), status.ConditionTypeModulesReady)
	g.Expect(modulesReady).ShouldNot(BeNil())
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionFalse))
}
