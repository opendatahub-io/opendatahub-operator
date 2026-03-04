package flags_test

import (
	"testing"

	"github.com/spf13/viper"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"

	. "github.com/onsi/gomega"
)

func TestRegisterComponentSuppressionFlag(t *testing.T) {
	t.Cleanup(func() { viper.Reset() })
	g := NewWithT(t)

	err := flags.RegisterComponentSuppressionFlags([]string{"dashboard"})
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(flags.IsComponentEnabled("dashboard")).Should(BeTrue(), "should be enabled by default")

	viper.Set("disable-dashboard-component", true)
	g.Expect(flags.IsComponentEnabled("dashboard")).Should(BeFalse(), "should be disabled when flag is set")
}

func TestComponentSuppressionFlagEnvBinding(t *testing.T) {
	t.Cleanup(func() { viper.Reset() })
	g := NewWithT(t)

	err := flags.RegisterComponentSuppressionFlags([]string{"kserve"})
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Setenv("RHAI_DISABLE_KSERVE_COMPONENT", "true")
	g.Expect(flags.IsComponentEnabled("kserve")).Should(BeFalse(), "should be disabled when env var is set")
}

func TestRegisterServiceSuppressionFlag(t *testing.T) {
	t.Cleanup(func() { viper.Reset() })
	g := NewWithT(t)

	err := flags.RegisterServiceSuppressionFlags([]string{"auth"})
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(flags.IsServiceEnabled("auth")).Should(BeTrue(), "should be enabled by default")

	viper.Set("disable-auth-service", true)
	g.Expect(flags.IsServiceEnabled("auth")).Should(BeFalse(), "should be disabled when flag is set")
}

func TestServiceSuppressionFlagEnvBinding(t *testing.T) {
	t.Cleanup(func() { viper.Reset() })
	g := NewWithT(t)

	err := flags.RegisterServiceSuppressionFlags([]string{"monitoring"})
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Setenv("RHAI_DISABLE_MONITORING_SERVICE", "true")
	g.Expect(flags.IsServiceEnabled("monitoring")).Should(BeFalse(), "should be disabled when env var is set")
}
