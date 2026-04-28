package eks_test

import (
	"testing"

	ccmtest "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/cloudmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

var tc *testf.TestContext

func TestMain(m *testing.M) {
	ccmtest.RunTestMain(m, &tc, eksCfg)
}
