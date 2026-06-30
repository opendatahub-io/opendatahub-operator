//nolint:testpackage
package precondition

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func newRunlevelRR(kind string) *types.ReconciliationRequest {
	return newRunlevelRRWithVersion("3.5.0", kind)
}

func newRunlevelRRWithVersion(ver string, kind string) *types.ReconciliationRequest {
	sv, _ := semver.Parse(ver)

	instance := &scheme.TestPlatformObject{
		TypeMeta: metav1.TypeMeta{Kind: kind},
		ObjectMeta: metav1.ObjectMeta{
			Name: xid.New().String(),
		},
	}

	return &types.ReconciliationRequest{
		Instance:   instance,
		Release:    common.Release{Version: version.OperatorVersion{Version: sv}},
		Conditions: cond.NewManager(instance, "Ready", PlatformReadyConditionType),
	}
}

func resetRunlevelState() {
	provision.DefaultRegistry().Reset()
	provision.GetRunlevelTracker().Reset()
}

func TestRunlevelGateAction_SetsSkipDeploy(t *testing.T) {
	tests := []struct {
		name         string
		component    string
		kind         string
		order        int
		trackerVer   string
		trackerOrder int
		rrVersion    string
	}{
		{
			name:      "tracker empty",
			component: "dashboard", kind: "Dashboard", order: 20,
			rrVersion: "3.5.0",
		},
		{
			name:      "version mismatch",
			component: "dashboard", kind: "Dashboard", order: 20,
			trackerVer: "3.4.0", trackerOrder: 31,
			rrVersion: "3.5.0",
		},
		{
			name:      "higher runlevel not yet cleared",
			component: "kserve", kind: "Kserve", order: 31,
			trackerVer: "3.5.0", trackerOrder: 20,
			rrVersion: "3.5.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRunlevelState()
			t.Cleanup(resetRunlevelState)
			g := NewWithT(t)

			provision.DefaultRegistry().Add(tt.component, provision.KindComponent, dag.Runlevel{Order: tt.order})
			if tt.trackerVer != "" {
				provision.GetRunlevelTracker().MarkCleared(tt.trackerVer, tt.trackerOrder)
			}

			rr := newRunlevelRRWithVersion(tt.rrVersion, tt.kind)
			err := RunlevelGateAction()(t.Context(), rr)

			var requeueErr odherrors.RequeueAfterError
			g.Expect(errors.As(err, &requeueErr)).To(BeTrue())
			g.Expect(requeueErr.After).To(Equal(30 * time.Second))
			g.Expect(rr.SkipDeploy).To(BeTrue())

			got := rr.Conditions.GetCondition(PlatformReadyConditionType)
			g.Expect(got).NotTo(BeNil())
			g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(got.Severity).To(Equal(common.ConditionSeverityInfo))
		})
	}
}

func TestRunlevelGateAction_TrackerCleared_NoSkipDeploy(t *testing.T) {
	resetRunlevelState()
	t.Cleanup(resetRunlevelState)
	g := NewWithT(t)

	provision.DefaultRegistry().Add("dashboard", provision.KindComponent, dag.Runlevel{Order: 20})
	provision.GetRunlevelTracker().MarkCleared("3.5.0", 20)

	rr := newRunlevelRR("Dashboard")
	err := RunlevelGateAction()(t.Context(), rr)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.SkipDeploy).To(BeFalse())

	got := rr.Conditions.GetCondition(PlatformReadyConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityInfo))
}

func TestRunlevelGateAction_NotInDAG_NoSkipDeploy(t *testing.T) {
	resetRunlevelState()
	t.Cleanup(resetRunlevelState)
	g := NewWithT(t)

	rr := newRunlevelRR("UnknownComponent")
	err := RunlevelGateAction()(t.Context(), rr)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.SkipDeploy).To(BeFalse())

	got := rr.Conditions.GetCondition(PlatformReadyConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
}

func TestRunlevelGateAction_OnlyReturnsRequeueOrNil(t *testing.T) {
	resetRunlevelState()
	t.Cleanup(resetRunlevelState)
	g := NewWithT(t)

	provision.DefaultRegistry().Add("dashboard", provision.KindComponent, dag.Runlevel{Order: 20})

	rr := newRunlevelRR("Dashboard")
	err := RunlevelGateAction()(t.Context(), rr)
	var requeueErr odherrors.RequeueAfterError
	g.Expect(errors.As(err, &requeueErr)).To(BeTrue())

	provision.GetRunlevelTracker().MarkCleared("3.5.0", 20)
	rr2 := newRunlevelRR("Dashboard")
	err = RunlevelGateAction()(t.Context(), rr2)
	g.Expect(err).NotTo(HaveOccurred())
}

// TestRunlevelGateAction_ActionChain_BlocksResourceDeployment simulates the
// full reconciler action pipeline (gate → render → deploy) and proves that
// when the runlevel is not cleared:
//   - the render action does not populate rr.Resources
//   - the deploy action does not process any resources
//
// After the runlevel is cleared, a second run proves both actions execute
// and the marker annotation from the "new manifest" reaches the deploy step.
func TestRunlevelGateAction_ActionChain_BlocksResourceDeployment(t *testing.T) {
	resetRunlevelState()
	t.Cleanup(resetRunlevelState)
	g := NewWithT(t)

	provision.DefaultRegistry().Add("kserve", provision.KindComponent, dag.Runlevel{Order: 31})
	provision.GetRunlevelTracker().MarkCleared("3.5.0", 20)

	var deployedResources []unstructured.Unstructured

	renderAction := func(_ context.Context, rr *types.ReconciliationRequest) error {
		if rr.SkipDeploy {
			return nil
		}

		obj := unstructured.Unstructured{}
		obj.SetAPIVersion("v1")
		obj.SetKind("ConfigMap")
		obj.SetName("inferenceservice-config")
		obj.SetNamespace("opendatahub")
		obj.SetAnnotations(map[string]string{
			"manifest-version": "3.5.0-upgrade",
		})

		rr.Resources = append(rr.Resources, obj)

		return nil
	}

	deployAction := func(_ context.Context, rr *types.ReconciliationRequest) error {
		if rr.SkipDeploy {
			return nil
		}

		deployedResources = append(deployedResources, rr.Resources...)

		return nil
	}

	chain := []actions.Fn{RunlevelGateAction(), renderAction, deployAction}

	// --- Run 1: runlevel 31 NOT cleared (only 20 is cleared) ---
	rr := newRunlevelRR("Kserve")
	runActionChain(t, g, chain, rr)

	g.Expect(rr.SkipDeploy).To(BeTrue(), "gate should have set SkipDeploy")
	g.Expect(rr.Resources).To(BeEmpty(), "render should have been skipped — no resources produced")
	g.Expect(deployedResources).To(BeEmpty(), "deploy should have been skipped — nothing applied")

	platReady := rr.Conditions.GetCondition(PlatformReadyConditionType)
	g.Expect(platReady).NotTo(BeNil())
	g.Expect(platReady.Status).To(Equal(metav1.ConditionFalse))

	// --- Run 2: runlevel 31 IS cleared ---
	provision.GetRunlevelTracker().MarkCleared("3.5.0", 31)
	deployedResources = nil

	rr2 := newRunlevelRR("Kserve")
	runActionChain(t, g, chain, rr2)

	g.Expect(rr2.SkipDeploy).To(BeFalse(), "gate should NOT have set SkipDeploy")
	g.Expect(rr2.Resources).To(HaveLen(1), "render should have produced resources")
	g.Expect(deployedResources).To(HaveLen(1), "deploy should have applied resources")
	g.Expect(deployedResources[0].GetAnnotations()).To(
		HaveKeyWithValue("manifest-version", "3.5.0-upgrade"),
		"the new manifest's marker annotation should reach the deploy step",
	)

	platReady2 := rr2.Conditions.GetCondition(PlatformReadyConditionType)
	g.Expect(platReady2).NotTo(BeNil())
	g.Expect(platReady2.Status).To(Equal(metav1.ConditionTrue))
}

// runActionChain executes actions in order, mirroring the reconciler's
// handling of RequeueAfterError (continue to next action).
func runActionChain(t *testing.T, g Gomega, chain []actions.Fn, rr *types.ReconciliationRequest) {
	t.Helper()

	for _, action := range chain {
		err := action(t.Context(), rr)

		var requeueErr odherrors.RequeueAfterError
		if errors.As(err, &requeueErr) {
			continue
		}

		g.Expect(err).NotTo(HaveOccurred())
	}
}
