package dsc

import (
	"context"
	"testing"
	"time"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

const (
	webhookSuiteDSCName          = "default-dsc"
	webhookSuiteOperationTimeout = 2 * time.Minute
)

type WebhookSuite struct {
	testContext *testf.TestContext
}

type AIGatewaySuite struct {
	WebhookSuite
}

type DataSuite struct {
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
	t.Run("trainingoperator", TrainingOperatorSuite{WebhookSuite: baseSuite}.run)
	t.Run("llamastackoperator", LlamaStackOperatorSuite{WebhookSuite: baseSuite}.run)
}

func (m WebhookSuite) newScenario(t *testing.T) *testf.WithT {
	t.Helper()

	withT := m.testContext.NewWithT(t)

	withT.DeleteObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Eventually().
		WithTimeout(webhookSuiteOperationTimeout).
		WithPolling(time.Second).
		Should(
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		)

	t.Cleanup(func() {
		withT.DeleteObject(&dscv2.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		}).Eventually().
			WithContext(context.Background()).
			WithTimeout(webhookSuiteOperationTimeout).
			WithPolling(time.Second).
			Should(
				MatchError(k8serr.IsNotFound, "IsNotFound"),
			)
	})

	return withT
}

func assertFieldsAbsentFromV3(t *testing.T, w *testf.WithT, fields ...string) {
	t.Helper()
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(dscv3.GroupVersion.WithKind("DataScienceCluster"))
	object.SetName(webhookSuiteDSCName)
	g := NewWithT(t)
	g.Expect(w.Client().Get(t.Context(), client.ObjectKeyFromObject(object), object)).To(Succeed())
	for _, field := range fields {
		for _, root := range []string{"spec", "status"} {
			_, found, err := unstructured.NestedFieldNoCopy(object.Object, root, "components", field)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeFalse(), "v3 object must not contain %s.components.%s", root, field)
		}
	}
}
