package conversion

import (
	"testing"
	"time"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/e2e/webhooks/conversion/dsc"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/e2e/webhooks/conversion/platform"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

// Run exercises the enabled DSC and Platform conversion scenarios through the installed webhooks.
func Run(t *testing.T, runDSC, runPlatform bool) {
	t.Helper()
	g := NewWithT(t)

	testContext, err := testf.NewTestContext(
		testf.WithTOptions(
			testf.WithEventuallyTimeout(5*time.Second),
			testf.WithEventuallyPollingInterval(time.Second),
		),
	)
	g.Expect(err).NotTo(HaveOccurred())

	if runDSC {
		t.Run("dsc", func(t *testing.T) {
			dsc.Run(t, testContext)
		})
	}
	if runPlatform {
		t.Run("platform", func(t *testing.T) {
			platform.Run(t, testContext)
		})
	}
}
