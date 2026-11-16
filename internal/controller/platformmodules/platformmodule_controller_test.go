//nolint:testpackage // Exercises package-private event routing directly.
package platformmodules

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestPlatformDAGAdvanceRequestsTargetsOnlyNewlyEligibleRunlevel(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)

	addRunlevelModule("rl20", 20)
	addRunlevelModule("rl31", 31)

	oldPlatform := platformWithDAG(20)
	newPlatform := oldPlatform.DeepCopy()
	newPlatform.Status.DAG.ClearedRunlevel = 31

	requests := platformDAGAdvanceRequests(oldPlatform, newPlatform)
	g.Expect(requests).To(HaveLen(1))
	g.Expect(requests[0].Name).To(Equal("rl31"))
}

func TestPlatformDAGAdvanceRequestsStartsOnlyFirstRunlevelForNewRelease(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)

	addRunlevelModule("rl20", 20)
	addRunlevelModule("rl31", 31)

	oldPlatform := platformWithDAG(31)
	newPlatform := oldPlatform.DeepCopy()
	newPlatform.Status.DAG.Release = testRelease("2.0.0")
	newPlatform.Status.DAG.ClearedRunlevel = 20

	requests := platformDAGAdvanceRequests(oldPlatform, newPlatform)
	g.Expect(requests).To(HaveLen(1))
	g.Expect(requests[0].Name).To(Equal("rl20"))
}

func TestPlatformDAGAdvanceRequestsSupportsRunlevelZero(t *testing.T) {
	for _, name := range []string{"only runlevel zero", "with later runlevel"} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			withCleanRegistries(t)
			addRunlevelModule("rl0", 0)
			if name == "with later runlevel" {
				addRunlevelModule("rl20", 20)
			}
			oldPlatform := platformWithDAG(0)
			newPlatform := oldPlatform.DeepCopy()
			newPlatform.Status.DAG.Release = testRelease("2.0.0")

			requests := platformDAGAdvanceRequests(oldPlatform, newPlatform)
			g.Expect(requests).To(HaveLen(1))
			g.Expect(requests[0].Name).To(Equal("rl0"))
			g.Expect(platformDAGAdvanceRequests(newPlatform, newPlatform.DeepCopy())).To(BeEmpty())

			cli, err := fakeclient.New(fakeclient.WithObjects(newPlatform))
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtype.ReconciliationRequest{Client: cli, Instance: &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "rl0"}}, Release: newPlatform.Status.DAG.Release}
			g.Expect(checkPlatformModuleRunlevelGate(t.Context(), rr)).To(Succeed())
			g.Expect(rr.SkipDeploy).To(BeFalse())
		})
	}
}

func addRunlevelModule(name string, runlevel int) {
	// Handlers define module behavior; the unified registry defines DAG ordering.
	modules.Add(
		&stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{
			Name: name,
			GVK:  schema.GroupVersionKind{Kind: name},
		}}},
	)
	provision.DefaultRegistry().Add(name, provision.KindModule, dag.RL(runlevel))
}

func platformWithDAG(clearedRunlevel int) *configApi.Platform {
	return &configApi.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName},
		Status: configApi.PlatformStatus{DAG: configApi.PlatformDAGStatus{
			Release:         testRelease("1.0.0"),
			ClearedRunlevel: clearedRunlevel,
		}},
	}
}

func TestPlatformDAGAdvanceRequestsIgnoresMissingRelease(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)
	addRunlevelModule("rl0", 0)
	oldPlatform := platformWithDAG(0)
	newPlatform := &configApi.Platform{}
	g.Expect(platformDAGAdvanceRequests(oldPlatform, newPlatform)).To(BeEmpty())
}
