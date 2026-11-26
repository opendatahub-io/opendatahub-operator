//nolint:testpackage // Exercises tracker readiness and aggregate status together.
package modules

import (
	"context"
	"errors"
	"testing"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	"github.com/spf13/viper"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestTrackerReadinessAndPlatformAggregate(t *testing.T) {
	target := common.Release{
		Name:    cluster.OpenDataHub,
		Version: ofversion.OperatorVersion{Version: semver.MustParse("1.2.3")},
	}
	old := target
	old.Version = ofversion.OperatorVersion{Version: semver.MustParse("1.2.2")}
	otherPlatform := target
	otherPlatform.Name = cluster.XKS

	for _, tc := range []struct {
		name           string
		missing        bool
		ready          metav1.ConditionStatus
		release        common.Release
		moduleRelease  string
		dagReady       bool
		aggregateReady bool
		upgradePending bool
	}{
		{name: "missing", missing: true},
		{name: "empty release ready", ready: metav1.ConditionTrue, upgradePending: true},
		{name: "fresh tracker without a release"},
		{name: "empty release not ready", ready: metav1.ConditionFalse},
		{name: "current ready", ready: metav1.ConditionTrue, release: target, dagReady: true, aggregateReady: true},
		{name: "runtime failure after handshake", ready: metav1.ConditionFalse, release: target, dagReady: true},
		{name: "previous release", ready: metav1.ConditionTrue, release: old, upgradePending: true},
		{name: "different platform", ready: metav1.ConditionTrue, release: otherPlatform, dagReady: true, upgradePending: true},
		// Operator resources committed at the target release (tracker), but
		// the module CR still reports the previous release: the module
		// operator is not converging (dead pods, quota-blocked scheduling)
		// and the batch must stay gated.
		{name: "module CR stuck at previous release", ready: metav1.ConditionTrue, release: target, moduleRelease: old.Version.String(), aggregateReady: true},
		// The module CR converged at the target release as well: gate opens.
		{name: "module CR converged at target release", ready: metav1.ConditionTrue, release: target, moduleRelease: target.Version.String(), dagReady: true, aggregateReady: true},
		// Module operators that never report a release (first deploy,
		// pre-feature CR) do not block the gate.
		{name: "module CR without release entry", ready: metav1.ConditionTrue, release: target, moduleRelease: "", dagReady: true, aggregateReady: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			withTestRegistry(t)
			stub := provisioningModuleStub{moduleName: "module", enabled: true}
			if tc.moduleRelease != "" || tc.name == "module CR without release entry" {
				stub.status = &ModuleStatus{ReleaseVersion: tc.moduleRelease}
			}
			DefaultRegistry().Add(stub)
			var objects []client.Object
			if !tc.missing {
				tracker := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "module"}}
				tracker.Status.Release = tc.release
				tracker.SetConditions([]common.Condition{{Type: status.ConditionTypeReady, Status: tc.ready}})
				objects = append(objects, tracker)
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
			g.Expect(err).ToNot(HaveOccurred())
			ready, err := NewReadinessChecker(DefaultRegistry(), cli, &configApi.PlatformModules{}, target.Version.String()).IsReady(t.Context(), "module")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(ready).To(Equal(tc.dagReady))

			platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName, UID: "platform-uid"}}
			rr := &odhtype.ReconciliationRequest{
				Client: cli, Instance: platform, Release: target,
				Conditions: conditionstest.NewManager(platform, status.ConditionTypeModulesReady),
			}
			g.Expect(aggregatePlatformModulesStatus(t.Context(), rr)).To(Succeed())
			condition := conditions.FindStatusCondition(platform, status.ConditionTypeModulesReady)
			g.Expect(condition).ToNot(BeNil())
			if tc.aggregateReady {
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			} else {
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			}
			if tc.upgradePending {
				g.Expect(condition.Message).To(And(
					ContainSubstring("upgrade pending"), ContainSubstring(tc.release.Version.String()), ContainSubstring(target.Version.String()),
				))
			}
		})
	}
}

func TestCoordinateDAGRestoresCheckpointAfterRestart(t *testing.T) {
	g := NewWithT(t)
	withTestRegistry(t)
	previous := viper.Get("disable-dag-ordering")
	viper.Set("disable-dag-ordering", false)
	t.Cleanup(func() { viper.Set("disable-dag-ordering", previous) })
	target := common.Release{
		Name:    cluster.OpenDataHub,
		Version: ofversion.OperatorVersion{Version: semver.MustParse("1.2.3")},
	}
	for name, order := range map[string]int{"first": 20, "second": 31} {
		DefaultRegistry().Add(provisioningModuleStub{moduleName: name, enabled: true})
		provision.DefaultRegistry().Add(name, provision.KindModule, dag.RL(order))
	}
	tracker := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "first"}}
	tracker.Status.Release = target
	tracker.SetConditions([]common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionFalse}})
	cli, err := fakeclient.New(fakeclient.WithObjects(tracker))
	g.Expect(err).ToNot(HaveOccurred())
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName, UID: "platform-uid"}}
	platform.Status.DAG.Release = target
	platform.Status.DAG.ClearedRunlevel = 20
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: platform, Release: target,
		Conditions: conditionstest.NewManager(platform, status.ConditionTypeProvisioningProgress),
	}
	// The in-memory runlevel tracker is empty, as after an operator restart.
	g.Expect(coordinateDAG(t.Context(), rr)).To(Succeed())
	g.Expect(platform.Status.DAG.ClearedRunlevel).To(Equal(31))
	g.Expect(platform.Status.DAG.Release).To(Equal(target))
}

func TestCoordinateDAGModuleReadiness(t *testing.T) {
	previous := viper.Get("disable-dag-ordering")
	viper.Set("disable-dag-ordering", false)
	t.Cleanup(func() { viper.Set("disable-dag-ordering", previous) })
	for _, tc := range []struct {
		name        string
		managed     bool
		registry    bool
		terminating bool
		want        int
	}{
		{name: "removed module", registry: true, want: 31},
		{name: "registry disabled", managed: true, want: 31},
		{name: "managed module awaiting tracker", managed: true, registry: true, want: 20},
		{name: "managed module with terminating tracker", managed: true, registry: true, terminating: true, want: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			withTestRegistry(t)
			DefaultRegistry().Add(provisioningModuleStub{moduleName: "first", enabled: tc.managed})
			DefaultRegistry().Add(provisioningModuleStub{moduleName: "second", enabled: true})
			if !tc.registry {
				DefaultRegistry().Disable("first")
			}
			provision.DefaultRegistry().Add("first", provision.KindModule, dag.RL(20))
			provision.DefaultRegistry().Add("second", provision.KindModule, dag.RL(31))
			platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName}}
			cli, err := fakeclient.New()
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtype.ReconciliationRequest{
				Client: cli, Instance: platform,
				Release:    common.Release{Version: ofversion.OperatorVersion{Version: semver.MustParse("1.2.3")}},
				Conditions: conditionstest.NewManager(platform, status.ConditionTypeProvisioningProgress),
			}
			if tc.terminating {
				platform.Status.DAG.Release = rr.Release
				platform.Status.DAG.ClearedRunlevel = 20
				tracker := &configApi.PlatformModule{
					ObjectMeta: metav1.ObjectMeta{Name: "first", Finalizers: []string{"test-finalizer"}},
					Status:     configApi.PlatformModuleStatus{Release: rr.Release},
				}
				g.Expect(cli.Create(t.Context(), tracker)).To(Succeed())
				g.Expect(cli.Delete(t.Context(), tracker)).To(Succeed())
			}
			err = coordinateDAG(t.Context(), rr)
			g.Expect(platform.Status.DAG.ClearedRunlevel).To(BeNumerically("==", tc.want))
			if tc.want == 31 {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(HaveOccurred())
			}
		})
	}
}

func TestPlatformReportsDisabledTrackerRemoval(t *testing.T) {
	g := NewWithT(t)
	withTestRegistry(t)
	DefaultRegistry().Add(provisioningModuleStub{moduleName: "module", enabled: false})
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName, UID: "platform-uid"}}
	tracker := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "module", Finalizers: []string{"test-finalizer"}, OwnerReferences: []metav1.OwnerReference{{
		APIVersion: configApi.GroupVersion.String(), Kind: configApi.PlatformKind, Name: platform.Name, UID: platform.UID, Controller: new(true),
	}}}}
	tracker.SetConditions([]common.Condition{{Type: status.ConditionTypeReady, Status: metav1.ConditionFalse, Message: "waiting for user/GitOps to delete module CR"}})
	cli, err := fakeclient.New(fakeclient.WithObjects(tracker))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cli.Delete(t.Context(), tracker)).To(Succeed())
	rr := &odhtype.ReconciliationRequest{Client: cli, Instance: platform, Conditions: conditionstest.NewManager(platform, status.ConditionTypeModulesReady)}
	g.Expect(aggregatePlatformModulesStatus(t.Context(), rr)).To(Succeed())
	condition := conditions.FindStatusCondition(platform, status.ConditionTypeModulesReady)
	g.Expect(condition).ToNot(BeNil())
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Message).To(ContainSubstring("user/GitOps"))
}

func TestTrackerLookupDistinguishesAbsentObjectFromAPIFailures(t *testing.T) {
	resource := configApi.GroupVersion.WithResource("platformmodules").GroupResource()
	for _, tc := range []struct {
		name      string
		err       error
		wantError bool
	}{
		{name: "tracker not created", err: k8serr.NewNotFound(resource, "module")},
		{name: "tracker API unavailable", err: &meta.NoKindMatchError{GroupKind: configApi.GroupVersion.WithKind(configApi.PlatformModuleKind).GroupKind()}, wantError: true},
		{name: "read forbidden", err: k8serr.NewForbidden(resource, "module", errors.New("denied")), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			withTestRegistry(t)
			DefaultRegistry().Add(provisioningModuleStub{moduleName: "module", enabled: true})
			cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return tc.err
				},
			}))
			g.Expect(err).ToNot(HaveOccurred())
			ready, err := NewReadinessChecker(DefaultRegistry(), cli, &configApi.PlatformModules{}, "1.2.3").IsReady(t.Context(), "module")
			g.Expect(ready).To(BeFalse())
			if tc.wantError {
				g.Expect(err).To(MatchError(tc.err))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
			platform := &configApi.Platform{}
			rr := &odhtype.ReconciliationRequest{Client: cli, Instance: platform,
				Conditions: conditionstest.NewManager(platform, status.ConditionTypeModulesReady),
			}
			err = aggregatePlatformModulesStatus(t.Context(), rr)
			if tc.wantError {
				g.Expect(err).To(MatchError(tc.err))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(conditions.FindStatusCondition(platform, status.ConditionTypeModulesReady).Status).To(Equal(metav1.ConditionFalse))
			}
		})
	}
}
