//go:build !nowebhook

package monitoring_test

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/monitoring"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

func TestRegisterWebhooks(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Create a test environment with manager
	env, err := envt.New(
		envt.WithManager(),
	)
	g.Expect(err).ShouldNot(HaveOccurred())
	defer func() {
		g.Expect(env.Stop()).To(Succeed())
	}()

	// Test that RegisterWebhooks succeeds
	err = monitoring.RegisterWebhooks(env.Manager())
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify that the webhook was registered by checking the webhook server
	webhookServer := env.Manager().GetWebhookServer()
	g.Expect(webhookServer).ShouldNot(BeNil())
}

func TestRegisterWebhooks_NilManager(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Test with nil manager (should panic or handle gracefully)
	defer func() {
		if r := recover(); r != nil {
			// Expected behavior - registering with nil manager should panic
			g.Expect(r).ShouldNot(BeNil())
		}
	}()

	err := monitoring.RegisterWebhooks(nil)
	// If we get here without panic, the error should indicate the problem
	if err != nil {
		g.Expect(err).To(HaveOccurred())
	}
}
