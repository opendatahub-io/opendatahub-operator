//nolint:testpackage // Verifies the post-render gate barrier on a single tracker.
package platformmodules

import (
	"testing"

	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

func TestTrackerWaitsForSiblingUpgradeGateWithDAGDisabled(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)
	previous := viper.Get("disable-dag-ordering")
	viper.Set("disable-dag-ordering", true)
	t.Cleanup(func() { viper.Set("disable-dag-ordering", previous) })
	modules.Add(&stubModuleHandler{BaseHandler: modules.BaseHandler{
		Config: modules.ModuleConfig{Name: "sibling", ManifestDir: "gate-module"},
	}, enabled: true})
	provision.Add("testmod", provision.KindModule, dag.RL(20))

	const namespace = "gate-test-operator"
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"}}
	dsci.Spec.ApplicationsNamespace = namespace
	platform := platformWithDAG(0)
	platform.UID = "platform-uid"
	cli, err := fakeclient.New(fakeclient.WithObjects(platform, dsci))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster.Init(t.Context(), cli, operatorconfig.OperatorSettings{
		OperatorNamespace: namespace, PlatformType: string(cluster.XKS),
	})).To(Succeed())
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{
		Name: "testmod",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: configApi.GroupVersion.String(), Kind: configApi.PlatformKind,
			Name: platform.Name, UID: platform.UID, Controller: new(true),
		}},
	}}
	pm.Status.Release = testRelease("99.99.98")
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: pm, Release: testRelease("99.99.99"),
		ManifestsBasePath: "../modules/testdata",
		Controller:        mocks.NewMockController(func(*mocks.MockController) {}),
		Conditions:        conditionstest.NewManager(pm, status.ConditionTypeProvisioningProgress),
	}
	g.Expect(checkPlatformModuleRunlevelGate(t.Context(), rr)).To(Succeed())
	g.Expect(rr.SkipDeploy).To(BeFalse())
	g.Expect(checkModuleUpgradeGates(t.Context(), rr)).To(BeAssignableToTypeOf(odherrors.StopError{}))

	ack := &corev1.ConfigMap{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: gates.AcksConfigMap}, ack)).To(Succeed())
	ack.Data["ack-99.99.99-module-gate"] = "true"
	g.Expect(cli.Update(t.Context(), ack)).To(Succeed())
	g.Expect(checkModuleUpgradeGates(t.Context(), rr)).To(Succeed())
}

func TestTrackerChecksGatesAfterPreflightRenderFailure(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)
	cli, err := fakeclient.New(fakeclient.WithObjects(platformWithDAG(0)))
	g.Expect(err).NotTo(HaveOccurred())
	const namespace = "gate-test-operator"
	g.Expect(cluster.Init(t.Context(), cli, operatorconfig.OperatorSettings{
		OperatorNamespace: namespace, PlatformType: string(cluster.XKS),
	})).To(Succeed())
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "gate-module"}}
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: pm, Release: testRelease("99.99.99"),
		GateEntries: map[string]string{"ack-99.99.99-module-gate": "Review migration"},
		Conditions:  conditionstest.NewManager(pm, status.ConditionTypeProvisioningProgress),
	}
	g.Expect(checkModuleUpgradeGates(t.Context(), rr)).To(BeAssignableToTypeOf(odherrors.StopError{}))
	ack := &corev1.ConfigMap{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: gates.AcksConfigMap}, ack)).To(Succeed())
	ack.Data["ack-99.99.99-module-gate"] = "true"
	g.Expect(cli.Update(t.Context(), ack)).To(Succeed())
	g.Expect(checkModuleUpgradeGates(t.Context(), rr)).To(Succeed())
}

func TestTrackerChecksGatesWhenSiblingRenderFails(t *testing.T) {
	// Caller-level: a sibling module whose manifests cannot render must not
	// bypass this tracker's own collected acknowledgment gates. Discovery
	// records the failure, but the barrier still enforces every gate it
	// managed to collect.
	g := NewWithT(t)
	withCleanRegistries(t)
	modules.Add(&stubModuleHandler{BaseHandler: modules.BaseHandler{
		Config: modules.ModuleConfig{Name: "broken-sibling", ManifestDir: "missing-module"},
	}, enabled: true})
	const namespace = "gate-test-operator"
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"}}
	dsci.Spec.ApplicationsNamespace = namespace
	platform := platformWithDAG(0)
	platform.UID = "platform-uid"
	cli, err := fakeclient.New(fakeclient.WithObjects(platform, dsci))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster.Init(t.Context(), cli, operatorconfig.OperatorSettings{
		OperatorNamespace: namespace, PlatformType: string(cluster.XKS),
	})).To(Succeed())
	pm := &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "gate-module"}}
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: pm, Release: testRelease("99.99.99"),
		ManifestsBasePath: "../modules/testdata",
		GateEntries:       map[string]string{"ack-99.99.99-module-gate": "Review migration"},
		Conditions:        conditionstest.NewManager(pm, status.ConditionTypeProvisioningProgress),
	}
	g.Expect(checkModuleUpgradeGates(t.Context(), rr)).To(BeAssignableToTypeOf(odherrors.StopError{}))
	ack := &corev1.ConfigMap{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: gates.AcksConfigMap}, ack)).To(Succeed())
	ack.Data["ack-99.99.99-module-gate"] = "true"
	g.Expect(cli.Update(t.Context(), ack)).To(Succeed())
	g.Expect(checkModuleUpgradeGates(t.Context(), rr)).To(Succeed())
}

func TestTrackerSkipsUpgradeGatesWhenDeploymentDeferred(t *testing.T) {
	// A closed runlevel sets SkipDeploy, but its delayed requeue lets later
	// actions run. A nil client verifies this gate returns before any API access.
	g := NewWithT(t)
	g.Expect(checkModuleUpgradeGates(t.Context(), &odhtype.ReconciliationRequest{SkipDeploy: true})).To(Succeed())
}
