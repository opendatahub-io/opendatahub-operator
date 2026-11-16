//nolint:testpackage // Verifies the post-render gate barrier on a single tracker.
package platformmodules

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestTrackerChecksGatesAfterPreflightRenderFailure(t *testing.T) {
	g := NewWithT(t)
	cli, err := fakeclient.New()
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

func TestTrackerSkipsUpgradeGatesWhenDeploymentDeferred(t *testing.T) {
	// A closed runlevel sets SkipDeploy, but its delayed requeue lets later
	// actions run. A nil client verifies this gate returns before any API access.
	g := NewWithT(t)
	g.Expect(checkModuleUpgradeGates(t.Context(), &odhtype.ReconciliationRequest{SkipDeploy: true})).To(Succeed())
}
