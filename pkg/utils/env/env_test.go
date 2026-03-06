package env_test

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/env"

	. "github.com/onsi/gomega"
)

func TestGetOrDefault(t *testing.T) {
	g := NewWithT(t)

	t.Run("returns env value when set", func(t *testing.T) {
		t.Setenv("TEST_GET_OR_DEFAULT", "from-env")
		g.Expect(env.GetOrDefault("TEST_GET_OR_DEFAULT", "fallback")).Should(Equal("from-env"))
	})

	t.Run("returns default when env unset", func(t *testing.T) {
		t.Setenv("TEST_GET_OR_DEFAULT", "")
		g.Expect(env.GetOrDefault("TEST_GET_OR_DEFAULT", "fallback")).Should(Equal("fallback"))
	})

	t.Run("trims whitespace from env value", func(t *testing.T) {
		t.Setenv("TEST_GET_OR_DEFAULT", "  value-with-spaces  ")
		g.Expect(env.GetOrDefault("TEST_GET_OR_DEFAULT", "fallback")).Should(Equal("value-with-spaces"))
	})

	t.Run("returns default for whitespace-only env value", func(t *testing.T) {
		t.Setenv("TEST_GET_OR_DEFAULT", "   ")
		// Whitespace-only after trim becomes empty string, so should return default
		g.Expect(env.GetOrDefault("TEST_GET_OR_DEFAULT", "fallback")).Should(Equal("fallback"))
	})
}
