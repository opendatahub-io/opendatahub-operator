package env_test

import (
	"testing"

	"github.com/onsi/gomega"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/env"
)

func TestEnvOrDefault(t *testing.T) {
	t.Run("returns environment variable value when set", func(t *testing.T) {
		g := gomega.NewWithT(t)

		t.Setenv("TEST_ENV_OR_DEFAULT", "custom-value")

		result := env.GetOrDefault("TEST_ENV_OR_DEFAULT", "fallback")
		g.Expect(result).To(gomega.Equal("custom-value"))
	})

	t.Run("returns default value when env var is not set", func(t *testing.T) {
		g := gomega.NewWithT(t)

		result := env.GetOrDefault("TEST_ENV_UNSET_VAR", "fallback")
		g.Expect(result).To(gomega.Equal("fallback"))
	})

	t.Run("returns default value when env var is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)

		t.Setenv("TEST_ENV_EMPTY", "")

		result := env.GetOrDefault("TEST_ENV_EMPTY", "fallback")
		g.Expect(result).To(gomega.Equal("fallback"))
	})
}
