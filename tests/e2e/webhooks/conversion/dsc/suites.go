package dsc

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

const (
	dscName                      = "default-dsc"
	webhookSuiteOperationTimeout = 2 * time.Minute
)

var dscKey = types.NamespacedName{Name: dscName}

type WebhookSuite struct {
	testContext *testf.TestContext
}

type AIGatewaySuite struct {
	WebhookSuite
}

type DataSuite struct {
	WebhookSuite
}

type WorkbenchesSuite struct {
	WebhookSuite
}

type TrainingOperatorSuite struct {
	WebhookSuite
}

type LlamaStackOperatorSuite struct {
	WebhookSuite
}

// Run exercises all DSC conversion scenarios through the installed webhook.
func Run(t *testing.T, testContext *testf.TestContext) {
	t.Helper()

	baseSuite := WebhookSuite{testContext: testContext}
	t.Run("dashboard", DashboardSuite{WebhookSuite: baseSuite}.run)
	t.Run("aihub", AIHubSuite{WebhookSuite: baseSuite}.run)
	t.Run("ai-gateway", AIGatewaySuite{WebhookSuite: baseSuite}.run)
	t.Run("data", DataSuite{WebhookSuite: baseSuite}.run)
	t.Run("workbenches", WorkbenchesSuite{WebhookSuite: baseSuite}.run)
	t.Run("trainingoperator", TrainingOperatorSuite{WebhookSuite: baseSuite}.run)
	t.Run("llamastackoperator", LlamaStackOperatorSuite{WebhookSuite: baseSuite}.run)
}

func (m WebhookSuite) newScenario(t *testing.T) *testf.WithT {
	t.Helper()

	withT := m.testContext.NewWithT(t)

	withT.DeleteObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
	}).Eventually().
		WithTimeout(webhookSuiteOperationTimeout).
		WithPolling(time.Second).
		Should(Succeed())

	withT.Get(gvk.DataScienceClusterV2, dscKey).Eventually().
		WithTimeout(webhookSuiteOperationTimeout).
		WithPolling(time.Second).
		Should(BeNil())

	t.Cleanup(func() {
		withT.DeleteObject(&dscv2.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{Name: dscKey.Name},
		}).Eventually().
			WithContext(context.Background()).
			WithTimeout(webhookSuiteOperationTimeout).
			WithPolling(time.Second).
			Should(Succeed())

		withT.Get(gvk.DataScienceClusterV2, dscKey).Eventually().
			WithContext(context.Background()).
			WithTimeout(webhookSuiteOperationTimeout).
			WithPolling(time.Second).
			Should(BeNil())
	})

	return withT
}
